package agent

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"

	"stock_rag/internal/observability"
	"stock_rag/internal/router"
)

// CoordinatorSelectInput 协调器选择输入（在 RouteEngine 已判定为 agent 路径后调用）。
type CoordinatorSelectInput struct {
	MessageID      string
	ConversationID string
	CurrentMessage string
	RecentMessages []router.MessageContext
	Summary        string

	// 上游路由结果（可选，用于级联置信度与观测）
	RouteMode      router.RouteMode
	RouteConfidence float64
	RouteReason    string

	ExplicitCoordinator CoordinatorType
	LastCoordinator     CoordinatorType
	StockCode           string
	DocType             string
}

// CoordinatorSelectDecision 协调器选择结果（字段风格对齐 router.RouteDecision）。
type CoordinatorSelectDecision struct {
	ID                string
	ConversationID    string
	MessageID         string
	ClassifierType    string
	ClassifierVersion string
	PredictedType     CoordinatorType
	SelectedType      CoordinatorType
	Confidence        float64
	Reason            string
	Candidates        []CandidateCoordinator
	ComplexityScore   float64
	TriggeredFallback bool
	UserFollowUp      bool
	LatencyMs         int
	CreatedAt         time.Time
}

// CandidateCoordinator 候选协调器及置信度。
type CandidateCoordinator struct {
	Type       CoordinatorType
	Confidence float64
}

// CoordinatorSelectConfig 阈值与默认协调器（建议与 router.RouteConfig 同量级调参）。
type CoordinatorSelectConfig struct {
	// 高置信：直接采用预测，不做粘性覆盖
	HighConfidenceThreshold float64
	// 中置信：允许追问粘性；低于此且无粘性则 fallback
	MediumConfidenceThreshold float64
	// 默认协调器（全局 fallback）
	DefaultCoordinator CoordinatorType
	// 复杂度达到该值时优先 plan（长链路、多工具）
	ComplexityPlanThreshold float64
	// 复杂度低于该值时优先 supervisor（单轮委派即可）
	ComplexitySupervisorThreshold float64
	// 是否启用 LLM 分类器
	EnableLLMClassifier bool
}

// DefaultCoordinatorSelectConfig 生产建议初值（与 route_engine 默认 0.8/0.6 对齐）。
func DefaultCoordinatorSelectConfig() CoordinatorSelectConfig {
	return CoordinatorSelectConfig{
		HighConfidenceThreshold:       0.82,
		MediumConfidenceThreshold:     0.65,
		DefaultCoordinator:            CoordinatorTypeSupervisor,
		ComplexityPlanThreshold:       0.70,
		ComplexitySupervisorThreshold: 0.45,
		EnableLLMClassifier:           true,
	}
}

// CoordinatorRule 硬规则（对齐 router.Rule 形态）。
type CoordinatorRule struct {
	Name          string
	Type          CoordinatorType
	Keywords      []string
	Confidence    float64
	Reason        string
	MinComplexity float64 // 可选：命中关键词且复杂度不低于此才生效，0 表示不限制
}

// CoordinatorRuleMatcher 协调器硬规则匹配器。
type CoordinatorRuleMatcher interface {
	Match(input *CoordinatorSelectInput, complexity float64) ([]CoordinatorRuleMatch, error)
}

// CoordinatorRuleMatch 单条规则命中结果。
type CoordinatorRuleMatch struct {
	Type       CoordinatorType
	Confidence float64
	Reason     string
	RuleName   string
}

// CoordinatorLLMClassifier 可选 LLM 协调器分类器。
type CoordinatorLLMClassifier interface {
	Classify(ctx context.Context, input *CoordinatorSelectInput, complexity float64) (*CoordinatorLLMResult, error)
}

// CoordinatorLLMResult LLM 分类输出。
type CoordinatorLLMResult struct {
	Type       CoordinatorType
	Confidence float64
	Reason     string
	Candidates []CandidateCoordinator
}

// CoordinatorSelectStore 决策落库（观测 / A-B）。
type CoordinatorSelectStore interface {
	RecordDecision(ctx context.Context, decision *CoordinatorSelectDecision) error
}

// CoordinatorSelector 按用户提问选择 CoordinatorType（pipeline 同 RouteEngine）。
type CoordinatorSelector struct {
	config        CoordinatorSelectConfig
	ruleMatcher   CoordinatorRuleMatcher
	llmClassifier CoordinatorLLMClassifier
	store         CoordinatorSelectStore
}

// NewCoordinatorSelector 创建选择器。
func NewCoordinatorSelector(
	config CoordinatorSelectConfig,
	ruleMatcher CoordinatorRuleMatcher,
	llmClassifier CoordinatorLLMClassifier,
	store CoordinatorSelectStore,
) *CoordinatorSelector {
	return &CoordinatorSelector{
		config:        config,
		ruleMatcher:   ruleMatcher,
		llmClassifier: llmClassifier,
		store:         store,
	}
}

// Select 执行协调器选择。调用方应保证 RouteMode 已为 agent（或显式要求选协调器）。
func (s *CoordinatorSelector) Select(ctx context.Context, input *CoordinatorSelectInput) (result *CoordinatorSelectDecision, err error) {
	ctx, span := observability.StartSpan(ctx, "CoordinatorSelector.Select")
	defer func() {
		if result != nil {
			span.SetAttributes(
				attribute.String("coordinator.selected", string(result.SelectedType)),
				attribute.String("coordinator.classifier", result.ClassifierType),
				attribute.Float64("coordinator.confidence", result.Confidence),
				attribute.Float64("coordinator.complexity", result.ComplexityScore),
			)
		}
		span.End()
	}()

	start := time.Now()
	complexity := scoreComplexity(input)

	decision := CoordinatorSelectDecision{
		ConversationID:  input.ConversationID,
		MessageID:       input.MessageID,
		ComplexityScore: complexity,
		CreatedAt:       time.Now(),
	}

	// 1. 显式指定
	if input.ExplicitCoordinator != "" {
		decision.ClassifierType = "explicit"
		decision.PredictedType = input.ExplicitCoordinator
		decision.SelectedType = input.ExplicitCoordinator
		decision.Confidence = 1.0
		decision.Reason = "显式指定协调器"
		return s.finish(ctx, &decision, start)
	}

	// 2. 硬规则
	if s.ruleMatcher != nil {
		matches, err := s.ruleMatcher.Match(input, complexity)
		if err != nil {
			return nil, err
		}
		if len(matches) > 0 {
			best := bestCoordinatorRuleMatch(matches)
			decision.ClassifierType = "rule"
			decision.ClassifierVersion = best.RuleName
			decision.PredictedType = best.Type
			decision.SelectedType = best.Type
			decision.Confidence = best.Confidence
			decision.Reason = best.Reason
			return s.finish(ctx, &decision, start)
		}
	}

	// 3. 追问粘性（无规则时优先于复杂度/默认，对齐 RouteEngine 无 LLM 分支）
	if sticky := coordinatorStickiness(input); sticky != nil {
		decision.ClassifierType = sticky.classifierType
		decision.PredictedType = sticky.predicted
		decision.SelectedType = sticky.selected
		decision.Confidence = sticky.confidence
		decision.Reason = sticky.reason
		decision.UserFollowUp = true
		return s.finish(ctx, &decision, start)
	}

	// 4. LLM 分类 + 置信度 / 二次粘性
	if s.config.EnableLLMClassifier && s.llmClassifier != nil {
		classResult, err := s.llmClassifier.Classify(ctx, input, complexity)
		if err != nil {
			return s.fallback(ctx, &decision, start, "llm_error", err.Error())
		}
		decision.ClassifierType = "llm"
		decision.PredictedType = classResult.Type
		decision.Confidence = classResult.Confidence
		decision.Reason = classResult.Reason
		decision.Candidates = classResult.Candidates
		s.applyConfidenceAndStickiness(&decision, input)
		return s.finish(ctx, &decision, start)
	}

	// 5. 复杂度启发（无 LLM 时的快速路径）
	if picked, ok := s.selectByComplexity(complexity); ok {
		decision.ClassifierType = "complexity"
		decision.PredictedType = picked
		decision.SelectedType = picked
		decision.Confidence = complexityConfidence(complexity, s.config)
		decision.Reason = fmt.Sprintf("复杂度得分 %.2f", complexity)
		return s.finish(ctx, &decision, start)
	}

	return s.fallback(ctx, &decision, start, "default", "使用默认协调器")
}

func (s *CoordinatorSelector) selectByComplexity(score float64) (CoordinatorType, bool) {
	switch {
	case score >= s.config.ComplexityPlanThreshold:
		return CoordinatorTypePlan, true
	case score >= s.config.ComplexitySupervisorThreshold:
		return CoordinatorTypeSupervisor, true
	default:
		return CoordinatorTypeSupervisor, true
	}
}

func (s *CoordinatorSelector) applyConfidenceAndStickiness(decision *CoordinatorSelectDecision, input *CoordinatorSelectInput) {
	decision.SelectedType = decision.PredictedType
	if decision.Confidence >= s.config.HighConfidenceThreshold {
		return
	}
	if sticky := coordinatorStickiness(input); sticky != nil {
		decision.ClassifierType = "llm+stickiness"
		decision.SelectedType = sticky.selected
		decision.Confidence = sticky.confidence
		decision.Reason = fmt.Sprintf("%s (%s)", decision.Reason, sticky.reason)
		decision.UserFollowUp = true
		return
	}
	if decision.Confidence >= s.config.MediumConfidenceThreshold {
		return
	}
	decision.SelectedType = s.config.DefaultCoordinator
	decision.TriggeredFallback = true
	decision.Reason += " (低置信度，回退默认协调器)"
}

func (s *CoordinatorSelector) fallback(ctx context.Context, decision *CoordinatorSelectDecision, start time.Time, classifier, reason string) (*CoordinatorSelectDecision, error) {
	decision.ClassifierType = classifier
	decision.PredictedType = s.config.DefaultCoordinator
	decision.SelectedType = s.config.DefaultCoordinator
	decision.Confidence = 0.5
	decision.TriggeredFallback = true
	decision.Reason = reason
	return s.finish(ctx, decision, start)
}

func (s *CoordinatorSelector) finish(ctx context.Context, decision *CoordinatorSelectDecision, start time.Time) (*CoordinatorSelectDecision, error) {
	decision.LatencyMs = int(time.Since(start).Milliseconds())
	if s.store != nil {
		if err := s.store.RecordDecision(ctx, decision); err != nil {
			observability.L().WarnCtx(ctx, "CoordinatorSelector failed to record decision", "error", err)
		}
	}
	return decision, nil
}

// --- 硬规则默认表（决策表 §3）---

// NewDefaultCoordinatorRuleMatcher 返回内置决策表规则。
func NewDefaultCoordinatorRuleMatcher() *HardCoordinatorRuleMatcher {
	return &HardCoordinatorRuleMatcher{rules: defaultCoordinatorRules()}
}

// HardCoordinatorRuleMatcher 关键词规则实现。
type HardCoordinatorRuleMatcher struct {
	rules []CoordinatorRule
}

func (m *HardCoordinatorRuleMatcher) Match(input *CoordinatorSelectInput, complexity float64) ([]CoordinatorRuleMatch, error) {
	text := normalizeCoordinatorText(input.CurrentMessage)
	var matches []CoordinatorRuleMatch
	for _, rule := range m.rules {
		if rule.MinComplexity > 0 && complexity < rule.MinComplexity {
			continue
		}
		for _, kw := range rule.Keywords {
			if strings.Contains(text, normalizeCoordinatorText(kw)) {
				matches = append(matches, CoordinatorRuleMatch{
					Type:       rule.Type,
					Confidence: rule.Confidence,
					Reason:     rule.Reason + ": " + kw,
					RuleName:   rule.Name,
				})
				break
			}
		}
	}
	return matches, nil
}

// defaultCoordinatorRules 决策表：规则名 → 协调器（优先级按 RouteEngine：先匹配先收集，bestRule 取最高 confidence）。
func defaultCoordinatorRules() []CoordinatorRule {
	return []CoordinatorRule{
		{
			Name:       "coordinator_plan",
			Type:       CoordinatorTypePlan,
			Keywords:   []string{"分步骤", "多步", "规划", "任务分解", "先查再", "连续步骤", "执行计划", "步骤1"},
			Confidence: 0.92,
			Reason:     "命中 Plan 关键词",
		},
		{
			Name:       "coordinator_pipeline",
			Type:       CoordinatorTypePipeline,
			Keywords:   []string{"固定流程", "标准流程", "按流程", "流水线", "依次", "先收集再分析", "检索后分析再总结"},
			Confidence: 0.88,
			Reason:     "命中固定串行流程关键词（映射 plan fixed steps）",
		},
		{
			Name:       "coordinator_workflow",
			Type:       CoordinatorTypeWorkflow,
			Keywords:   []string{"模板", "标准研报", "固定模板", "工作流", "SOP"},
			Confidence: 0.86,
			Reason:     "命中 Workflow 关键词",
		},
		{
			Name:       "coordinator_debate",
			Type:       CoordinatorTypeDebate,
			Keywords:   []string{"辩论", "正反", "多空", "争议", "看涨看跌", "支持与反对"},
			Confidence: 0.85,
			Reason:     "命中 Debate 关键词（映射 multi_agent debate）",
		},
		{
			Name:       "coordinator_committee",
			Type:       CoordinatorTypeCommittee,
			Keywords:   []string{"合议", "委员会", "投票", "一致意见", "综合各方"},
			Confidence: 0.85,
			Reason:     "命中 Committee 关键词（映射 multi_agent committee）",
		},
		{
			Name:       "coordinator_peer",
			Type:       CoordinatorTypePeer,
			Keywords:   []string{"并行分析", "独立分析", "多角度", "协商", "对等", "各自给出"},
			Confidence: 0.83,
			Reason:     "命中 Peer 关键词（映射 multi_agent parallel）",
		},
		{
			Name:       "coordinator_supervisor_compare",
			Type:       CoordinatorTypeSupervisor,
			Keywords:   []string{"对比", "相比", "比较", "vs", "交叉验证"},
			Confidence: 0.90,
			Reason:     "对比/验证类任务，主从调度",
			MinComplexity: 0.35,
		},
		{
			Name:       "coordinator_deep",
			Type:       CoordinatorTypeDeep,
			Keywords:   []string{"深度研究", "全面尽调", "深挖", "详细研报"},
			Confidence: 0.84,
			Reason:     "命中 Deep 关键词",
			MinComplexity: 0.55,
		},
	}
}

func bestCoordinatorRuleMatch(matches []CoordinatorRuleMatch) CoordinatorRuleMatch {
	best := matches[0]
	for _, m := range matches[1:] {
		if m.Confidence > best.Confidence {
			best = m
		}
	}
	return best
}

// scoreComplexity 复杂度启发分 [0,1]（对齐 PlannerAgent 信号 + route 规则）。
func scoreComplexity(input *CoordinatorSelectInput) float64 {
	msg := strings.TrimSpace(input.CurrentMessage)
	if msg == "" {
		return 0.3
	}
	score := 0.25
	text := normalizeCoordinatorText(msg)

	add := func(delta float64) { score += delta }
	if containsAny(text, []string{"对比", "相比", "比较", "vs", "交叉验证"}) {
		add(0.22)
	}
	if containsAny(text, []string{"和", "与", "以及", "多家", "两家公司"}) {
		add(0.12)
	}
	if yearCount(msg) >= 2 {
		add(0.15)
	}
	if containsAny(text, []string{"分步骤", "先", "然后", "再", "最后", "多步", "规划"}) {
		add(0.18)
	}
	if containsAny(text, []string{"研报", "公告", "财报", "多个来源", "验证", "核对"}) {
		add(0.10)
	}
	if input.StockCode != "" && containsAny(text, []string{"分析", "估值", "指标", "趋势"}) {
		add(0.08)
	}
	if len(input.RecentMessages) > 0 {
		add(0.05)
	}
	if score > 1 {
		return 1
	}
	return score
}

func complexityConfidence(score float64, cfg CoordinatorSelectConfig) float64 {
	switch {
	case score >= cfg.ComplexityPlanThreshold:
		return 0.82
	case score >= cfg.ComplexitySupervisorThreshold:
		return 0.76
	default:
		return 0.68
	}
}

type coordinatorStickyResult struct {
	classifierType string
	predicted      CoordinatorType
	selected       CoordinatorType
	confidence     float64
	reason         string
}

func coordinatorStickiness(input *CoordinatorSelectInput) *coordinatorStickyResult {
	if input.LastCoordinator == "" {
		return nil
	}
	if !isCoordinatorFollowUp(input) {
		return nil
	}
	return &coordinatorStickyResult{
		classifierType: "stickiness",
		predicted:      input.LastCoordinator,
		selected:       input.LastCoordinator,
		confidence:     0.78,
		reason:         "追问继承协调器: " + string(input.LastCoordinator),
	}
}

func isCoordinatorFollowUp(input *CoordinatorSelectInput) bool {
	msg := strings.TrimSpace(input.CurrentMessage)
	if msg == "" {
		return false
	}
	if containsAny(msg, []string{"换模式", "改用", "不要plan", "简单回答"}) {
		return false
	}
	phrases := []string{
		"继续", "接着说", "上一条", "刚才", "然后呢", "还有呢",
		"同上", "那个呢", "这个呢",
	}
	for _, p := range phrases {
		if strings.Contains(msg, p) {
			return true
		}
	}
	if utf8.RuneCountInString(msg) <= 32 && (strings.HasSuffix(msg, "呢") || strings.Contains(msg, "？")) {
		return len(input.RecentMessages) > 0 || strings.TrimSpace(input.Summary) != ""
	}
	return false
}

func normalizeCoordinatorText(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func containsAny(text string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, normalizeCoordinatorText(kw)) {
			return true
		}
	}
	return false
}

func yearCount(msg string) int {
	n := 0
	for i := 0; i+4 <= len(msg); i++ {
		chunk := msg[i : i+4]
		if chunk[0] >= '0' && chunk[0] <= '9' &&
			chunk[1] >= '0' && chunk[1] <= '9' &&
			chunk[2] >= '0' && chunk[2] <= '9' &&
			chunk[3] >= '0' && chunk[3] <= '9' {
			if i+7 <= len(msg) && msg[i+4:i+7] == "年" {
				n++
			}
		}
	}
	return n
}
