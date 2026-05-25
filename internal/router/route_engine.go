package router

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"

	"stock_rag/internal/observability"
	"stock_rag/internal/repository"

	"github.com/google/uuid"
)

type RouteMode string

const (
	ModeChat     RouteMode = "chat"
	ModeRAG      RouteMode = "rag"      // 已弃用，映射为 agent
	ModeAnalysis RouteMode = "analysis" // 已弃用，映射为 agent
	ModeAgent    RouteMode = "agent"
)

// NormalizeMode 规范化模式，将旧模式映射为新模式
func NormalizeMode(mode RouteMode) RouteMode {
	switch mode {
	case ModeRAG, ModeAnalysis:
		return ModeAgent
	default:
		return mode
	}
}

type RouteConfig struct {
	HighConfidenceThreshold   float64
	MediumConfidenceThreshold float64
	DefaultMode               RouteMode
	EnableAgent               bool
	EnableAnalysis            bool
}

func DefaultRouteConfig() RouteConfig {
	return RouteConfig{
		HighConfidenceThreshold:   0.8,
		MediumConfidenceThreshold: 0.6,
		DefaultMode:               ModeChat,
		EnableAgent:               true,
		EnableAnalysis:            true,
	}
}

type RouteInput struct {
	MessageID      string
	ConversationID string
	UserID         string
	CurrentMessage string
	RecentMessages []MessageContext
	Summary        string
	LastRouteMode  RouteMode
	ExplicitMode   RouteMode
	StockCode      string
	DocType        string
	TimeRange      string
}

type MessageContext struct {
	Role      string
	Content   string
	RouteMode RouteMode
	CreatedAt time.Time
}

type RouteDecision struct {
	ID                string
	ConversationID    string
	MessageID         string
	ClassifierType    string
	ClassifierVersion string
	PredictedMode     RouteMode
	SelectedMode      RouteMode
	Confidence        float64
	Reason            string
	Candidates        []CandidateMode
	Selected          bool
	TriggeredFallback bool
	ExecutionSuccess  bool
	LatencyMs         int
	UserFollowUp      bool
	CreatedAt         time.Time
}

type CandidateMode struct {
	Mode       RouteMode
	Confidence float64
}

type RouteEngine struct {
	config        RouteConfig
	llmClassifier LLMClassifier
	ruleMatcher   RuleMatcher
	store         RouteStore
}

type LLMClassifier interface {
	Classify(ctx context.Context, input *RouteInput) (*LLMClassificationResult, error)
}

type LLMClassificationResult struct {
	Mode       RouteMode
	Confidence float64
	Reason     string
	Candidates []CandidateMode
}

type RuleMatcher interface {
	Match(input *RouteInput) ([]RuleMatch, error)
}

type RuleMatch struct {
	Mode       RouteMode
	Confidence float64
	Reason     string
	RuleName   string
}

type RouteStore interface {
	RecordDecision(ctx context.Context, decision *RouteDecision) error
	GetDecisionsByConversation(ctx context.Context, conversationID string) ([]*RouteDecision, error)
	GetDecisionByMessage(ctx context.Context, messageID string) (*RouteDecision, bool)
}

func NewRouteEngine(config RouteConfig, llmClassifier LLMClassifier, ruleMatcher RuleMatcher, store RouteStore) *RouteEngine {
	return &RouteEngine{
		config:        config,
		llmClassifier: llmClassifier,
		ruleMatcher:   ruleMatcher,
		store:         store,
	}
}

func (e *RouteEngine) Route(ctx context.Context, input *RouteInput) (result *RouteDecision, err error) {
	ctx, span := observability.StartSpan(ctx, "RouteEngine.Route")
	defer func() {
		if result != nil {
			span.SetAttributes(
				attribute.String("route.selected_mode", string(result.SelectedMode)),
				attribute.String("route.classifier", result.ClassifierType),
				attribute.Float64("route.confidence", result.Confidence),
			)
		}
		span.End()
	}()
	startTime := time.Now()

	decision := RouteDecision{
		ID:             uuid.New().String(),
		ConversationID: input.ConversationID,
		MessageID:      input.MessageID,
		CreatedAt:      time.Now(),
		Selected:       true,
	}

	// 第一步：显式模式优先（兼容旧模式）
	if input.ExplicitMode != "" {
		decision.ClassifierType = "explicit"
		decision.PredictedMode = input.ExplicitMode
		decision.SelectedMode = NormalizeMode(input.ExplicitMode)
		decision.Confidence = 1.0
		decision.Reason = "显式指定模式"
		return e.finishRoute(ctx, &decision, startTime)
	}

	// 第二步：硬规则（意图明确时优先于会话粘性）
	ruleMatches, err := e.ruleMatcher.Match(input)
	if err != nil {
		return nil, err
	}
	if len(ruleMatches) > 0 {
		bestMatch := bestRuleMatch(ruleMatches)
		decision.ClassifierType = "rule"
		decision.ClassifierVersion = bestMatch.RuleName
		decision.PredictedMode = bestMatch.Mode
		decision.SelectedMode = NormalizeMode(bestMatch.Mode)
		decision.Confidence = bestMatch.Confidence
		decision.Reason = bestMatch.Reason
		return e.finishRoute(ctx, &decision, startTime)
	}

	// 第三步：LLM 分类 + 置信度/追问粘性
	if e.llmClassifier != nil {
		classResult, err := e.llmClassifier.Classify(ctx, input)
		if err != nil {
			return nil, err
		}

		decision.ClassifierType = "llm"
		decision.PredictedMode = classResult.Mode
		decision.Confidence = classResult.Confidence
		decision.Reason = classResult.Reason
		decision.Candidates = normalizeCandidates(classResult.Candidates)
		e.applyConfidenceAndStickiness(&decision, input)
		return e.finishRoute(ctx, &decision, startTime)
	}

	// 无 LLM：仅在明确追问时继承上一轮，否则默认模式
	if stickiness := stickinessDecision(input); stickiness != nil {
		decision.ClassifierType = stickiness.classifierType
		decision.PredictedMode = stickiness.predicted
		decision.SelectedMode = stickiness.selected
		decision.Confidence = stickiness.confidence
		decision.Reason = stickiness.reason
		decision.UserFollowUp = true
		return e.finishRoute(ctx, &decision, startTime)
	}

	decision.ClassifierType = "default"
	decision.PredictedMode = e.config.DefaultMode
	decision.SelectedMode = NormalizeMode(e.config.DefaultMode)
	decision.Confidence = 0.5
	decision.Reason = "使用默认模式"
	return e.finishRoute(ctx, &decision, startTime)
}

func (e *RouteEngine) applyConfidenceAndStickiness(decision *RouteDecision, input *RouteInput) {
	predicted := NormalizeMode(decision.PredictedMode)
	decision.SelectedMode = predicted

	if decision.Confidence >= e.config.HighConfidenceThreshold {
		return
	}

	if stickiness := stickinessDecision(input); stickiness != nil {
		decision.ClassifierType = "llm+stickiness"
		decision.SelectedMode = stickiness.selected
		decision.Confidence = stickiness.confidence
		decision.Reason = fmt.Sprintf("%s (%s)", decision.Reason, stickiness.reason)
		decision.UserFollowUp = true
		return
	}

	if decision.Confidence >= e.config.MediumConfidenceThreshold {
		// 中置信度且无追问信号：信任分类器预测，避免一律降级到 chat
		return
	}

	decision.SelectedMode = NormalizeMode(e.config.DefaultMode)
	decision.TriggeredFallback = true
	decision.Reason += " (低置信度且无追问上下文，使用默认模式)"
}

type stickinessResult struct {
	classifierType string
	predicted      RouteMode
	selected       RouteMode
	confidence     float64
	reason         string
}

func stickinessDecision(input *RouteInput) *stickinessResult {
	if !shouldApplyStickiness(input) {
		return nil
	}
	inherited := NormalizeMode(input.LastRouteMode)
	return &stickinessResult{
		classifierType: "stickiness",
		predicted:      input.LastRouteMode,
		selected:       inherited,
		confidence:     0.78,
		reason:         "追问继承会话模式: " + string(inherited),
	}
}

func shouldApplyStickiness(input *RouteInput) bool {
	if input.LastRouteMode == "" {
		return false
	}
	msg := strings.TrimSpace(input.CurrentMessage)
	if msg == "" {
		return false
	}
	if isCapabilityQuestion(msg) || hasModeSwitchIntent(msg) {
		return false
	}
	return isFollowUpMessage(input, msg)
}

func isCapabilityQuestion(msg string) bool {
	questions := []string{
		"你能帮我做什么", "你会什么", "你能做什么", "你可以做什么",
		"你有什么功能", "你的功能", "介绍一下你自己", "介绍一下你",
		"你是谁", "怎么使用", "你擅长什么", "我能问你什么",
	}
	for _, q := range questions {
		if strings.Contains(msg, q) {
			return true
		}
	}
	return false
}

// hasModeSwitchIntent 检测用户是否在发起新的任务类型（应走规则/LLM，而非继承）
func hasModeSwitchIntent(msg string) bool {
	text := normalizeText(msg)
	for _, kw := range modeSwitchKeywords {
		if strings.Contains(text, normalizeText(kw)) {
			return true
		}
	}
	return false
}

var modeSwitchKeywords = []string{
	"研报", "公告", "财报", "文档", "原文", "引用", "根据资料", "根据报告",
	"分析", "提取指标", "总结观点", "估值", "同比", "环比", "财务指标", "拆解", "趋势",
	"对比", "分步骤", "综合", "综合分析", "多维度", "多角度", "交叉验证",
	"帮我执行", "查多个", "连续步骤", "工具调用", "规划",
	"你是谁", "你能做什么", "你好", "谢谢", "闲聊",
}

func isFollowUpMessage(input *RouteInput, msg string) bool {
	if len(input.RecentMessages) == 0 && strings.TrimSpace(input.Summary) == "" {
		return false
	}

	continuationPhrases := []string{
		"上一条", "接着说", "继续问", "继续", "还有呢", "然后呢",
		"那这个", "那个呢", "这个呢", "它呢", "同上", "刚才说的",
		"前面说的", "前文", "上一轮", "接着", "再说说",
	}
	for _, phrase := range continuationPhrases {
		if strings.Contains(msg, phrase) {
			return true
		}
	}

	runeLen := utf8.RuneCountInString(msg)
	if runeLen <= 32 {
		if strings.HasSuffix(msg, "呢") || strings.HasSuffix(msg, "吗") ||
			strings.Contains(msg, "？") || strings.Contains(msg, "?") {
			return true
		}
	}

	if runeLen <= 40 {
		ellipsisHints := []string{"呢", "怎么样", "如何", "多少", "为什么", "什么意思"}
		for _, hint := range ellipsisHints {
			if strings.Contains(msg, hint) {
				return true
			}
		}
	}

	return false
}

func bestRuleMatch(matches []RuleMatch) RuleMatch {
	best := matches[0]
	for _, match := range matches[1:] {
		if match.Confidence > best.Confidence {
			best = match
		}
	}
	return best
}

func normalizeCandidates(candidates []CandidateMode) []CandidateMode {
	out := make([]CandidateMode, len(candidates))
	for i, c := range candidates {
		out[i] = CandidateMode{
			Mode:       NormalizeMode(c.Mode),
			Confidence: c.Confidence,
		}
	}
	return out
}

func (e *RouteEngine) finishRoute(ctx context.Context, decision *RouteDecision, startTime time.Time) (*RouteDecision, error) {
	decision.LatencyMs = int(time.Since(startTime).Milliseconds())
	if e.store != nil {
		if err := e.store.RecordDecision(ctx, decision); err != nil {
			return decision, err
		}
	}
	return decision, nil
}

type PostgresRouteStore struct {
	repo *repository.PostgresAuthRepositoryV2
}

func NewPostgresRouteStore(repo *repository.PostgresAuthRepositoryV2) *PostgresRouteStore {
	return &PostgresRouteStore{repo: repo}
}

func (s *PostgresRouteStore) RecordDecision(ctx context.Context, decision *RouteDecision) error {
	candidates := make([]map[string]interface{}, len(decision.Candidates))
	for i, c := range decision.Candidates {
		candidates[i] = map[string]interface{}{
			"mode":       c.Mode,
			"confidence": c.Confidence,
		}
	}

	repoDecision := &repository.RouteDecision{
		ID:                decision.ID,
		ConversationID:    decision.ConversationID,
		MessageID:         decision.MessageID,
		ClassifierType:    decision.ClassifierType,
		ClassifierVersion: decision.ClassifierVersion,
		PredictedMode:     string(decision.PredictedMode),
		Confidence:        decision.Confidence,
		Reason:            decision.Reason,
		Candidates:        "",
		Selected:          decision.Selected,
		CreatedAt:         decision.CreatedAt,
	}

	return s.repo.CreateRouteDecision(repoDecision)
}

func (s *PostgresRouteStore) GetDecisionsByConversation(ctx context.Context, conversationID string) ([]*RouteDecision, error) {
	return []*RouteDecision{}, nil
}

func (s *PostgresRouteStore) GetDecisionByMessage(ctx context.Context, messageID string) (*RouteDecision, bool) {
	return nil, false
}
