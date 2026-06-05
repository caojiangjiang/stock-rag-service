package service

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"stock_rag/internal/concurrency"
	"stock_rag/internal/eino/agent"
	"stock_rag/internal/llm"
	"stock_rag/internal/metrics"
	appmodel "stock_rag/internal/model"
	"stock_rag/internal/observability"
	"stock_rag/internal/persona/config"
	"stock_rag/internal/persona/model"
)

// QueryService 是 persona service 对查询服务的最小依赖接口
type QueryService interface {
	Query(ctx context.Context, req appmodel.RAGQueryRequest) (appmodel.RAGQueryResponse, error)
}

// ConfigPersonaService 基于配置的 Persona 服务实现
type ConfigPersonaService struct {
	loader              *config.PersonaConfigLoader
	queryService        QueryService
	coordinatorFactory  *agent.CoordinatorFactory
}

// NewConfigPersonaService 创建基于配置的 Persona 服务。
// coordinatorFactory 为 nil 时会创建默认工厂（圆桌使用 MultiAgentCoordinator debate 拓扑）。
func NewConfigPersonaService(configPath string, queryService QueryService, coordinatorFactory *agent.CoordinatorFactory) (*ConfigPersonaService, error) {
	loader, err := config.NewPersonaConfigLoader(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load persona config: %w", err)
	}
	if coordinatorFactory == nil {
		coordinatorFactory = newDefaultCoordinatorFactory()
	}
	return &ConfigPersonaService{
		loader:             loader,
		queryService:       queryService,
		coordinatorFactory: coordinatorFactory,
	}, nil
}

// NewConfigPersonaServiceWithDefaults 创建不带 QueryService 的版本（用于向后兼容和测试）
func NewConfigPersonaServiceWithDefaults(configPath string) (*ConfigPersonaService, error) {
	return NewConfigPersonaService(configPath, nil, nil)
}

func (s *ConfigPersonaService) ListPersonas(ctx context.Context, market, styleTag, status string, limit int) ([]model.PersonaCard, error) {
	return s.loader.ListPersonas(market, styleTag, status, limit)
}

func (s *ConfigPersonaService) GetPersona(ctx context.Context, personaID string) (*model.PersonaProfile, error) {
	profile, err := s.loader.GetPersona(personaID)
	if err != nil {
		return nil, ErrPersonaNotFound
	}
	return profile, nil
}

// Chat 实现真实的 Persona 单聊能力
func (s *ConfigPersonaService) Chat(ctx context.Context, req *model.PersonaChatRequest) (*model.PersonaAnswer, error) {
	startTime := time.Now()
	requestID := fmt.Sprintf("persona-chat-%d", time.Now().UnixNano())
	questionHash := hashString(req.Message)
	questionLen := len(req.Message)

	// 0. 安全检查
	if refusalReason := checkSafety(req.Message); refusalReason != "" {
		metrics.RecordPersonaChatRefusal(req.PersonaID, refusalReason)
		observability.L().WarnCtx(ctx, "Persona chat refused - safety check failed",
			"feature", "persona",
			"mode", "chat",
			"request_id", requestID,
			"persona_id", req.PersonaID,
			"question_len", questionLen,
			"question_hash", questionHash,
			"refusal_reason", refusalReason)

		// 返回安全拒答响应
		profile, _ := s.loader.GetPersona(req.PersonaID)
		var promptBuilder *PersonaPromptBuilder
		if profile != nil {
			promptBuilder = NewPersonaPromptBuilder(profile)
		}

		return &model.PersonaAnswer{
			PersonaID:   req.PersonaID,
			PersonaName: profile.Name,
			Summary:     "您的问题涉及安全敏感内容，无法提供投资分析。",
			Stance:      "出于安全考虑，暂不发表投资观点。",
			Thesis:    []string{"您的问题涉及安全敏感内容，无法提供回答。"},
			Citations: []model.EvidenceItem{},
			Disclaimer: promptBuilder.BuildDisclaimer(),
			RequestID:  requestID,
		}, nil
	}

	// 1. 获取 Persona Profile
	profile, err := s.loader.GetPersona(req.PersonaID)
	if err != nil {
		metrics.RecordPersonaChatError(req.PersonaID, "persona_not_found")
		observability.L().ErrorCtx(ctx, "Persona chat failed - persona not found", nil,
			"feature", "persona",
			"mode", "chat",
			"request_id", requestID,
			"persona_id", req.PersonaID,
			"question_len", questionLen,
			"question_hash", questionHash,
			"error_code", "persona_not_found")
		return nil, ErrPersonaNotFound
	}

	// 2. 创建 Prompt Builder
	promptBuilder := NewPersonaPromptBuilder(profile)

	// 直答模式：不检索知识库，仅按角色设定由大模型回答（适合观点讨论、宏观问题）
	if !shouldUseRAG(req) || s.queryService == nil {
		return s.chatDirect(ctx, profile, req, promptBuilder, requestID, startTime)
	}

	// 3. 检索用用户原问题（避免长角色包装干扰向量/关键词召回）；生成时用角色系统提示
	retrieveQuery := promptBuilder.BuildRetrieveQuery(req.Message)

	// 4. 调用 QueryService 获取 RAG 结果
	var ragAnswer string
	var citations []appmodel.Citation

	ragReq := appmodel.RAGQueryRequest{
		Question:     retrieveQuery,
		UserQuestion: strings.TrimSpace(req.Message),
		StockCode:    req.StockCode,
		TimeRange:    req.TimeRange,
		TopK:         5,
		SystemPrompt: promptBuilder.BuildPersonaSystemPrompt(),
	}

	ragResp, err := s.queryService.Query(ctx, ragReq)
	if err != nil {
		ragAnswer = "根据我的投资分析框架，该问题涉及多个关键因素需要综合考虑。"
		observability.L().WarnCtx(ctx, "Persona chat RAG query failed, using fallback",
			"feature", "persona",
			"mode", "chat",
			"request_id", requestID,
			"persona_id", req.PersonaID,
			"error", err.Error())
	} else {
		ragAnswer = ragResp.Answer
		citations = ragResp.Citations
	}

	// 5. 构建 PersonaAnswer（单聊不附带反方观点，反方对比留给圆桌讨论）
	summary := strings.TrimSpace(ragAnswer)
	answer := &model.PersonaAnswer{
		PersonaID:   req.PersonaID,
		PersonaName: profile.Name,
		Summary:     summary,
		Stance:      promptBuilder.BuildStance(req.Message, ragAnswer),
		Thesis:     promptBuilder.BuildThesis(ragAnswer),
		Citations:  MapCitationToEvidence(citations),
		Disclaimer:  promptBuilder.BuildDisclaimer(),
		RequestID:   requestID,
	}
	answer.Thesis = dedupeThesisAgainstSummary(summary, answer.Thesis)

	// 记录指标和日志
	latencyMs := time.Since(startTime).Milliseconds()
	citationCount := len(answer.Citations)

	metrics.RecordPersonaChatRequest("success", req.PersonaID, time.Since(startTime).Seconds(), citationCount)
	observability.L().InfoCtx(ctx, "Persona chat completed",
		"feature", "persona",
		"mode", "chat",
		"request_id", requestID,
		"persona_id", req.PersonaID,
		"stock_code", req.StockCode,
		"question_len", questionLen,
		"question_hash", questionHash,
		"citation_count", citationCount,
		"latency_ms", latencyMs,
		"status", "success")

	return answer, nil
}

func shouldUseRAG(req *model.PersonaChatRequest) bool {
	if req.UseRAG != nil {
		return *req.UseRAG
	}
	v := strings.TrimSpace(os.Getenv("PERSONA_CHAT_USE_RAG"))
	if v == "" {
		return true
	}
	return !strings.EqualFold(v, "false") && v != "0"
}

func (s *ConfigPersonaService) chatDirect(
	ctx context.Context,
	profile *model.PersonaProfile,
	req *model.PersonaChatRequest,
	promptBuilder *PersonaPromptBuilder,
	requestID string,
	startTime time.Time,
) (*model.PersonaAnswer, error) {
	systemPrompt := promptBuilder.BuildPersonaSystemPrompt() + "\n当前为角色对话模式，未检索实时公告/研报；请结合角色框架与公开逻辑分析，勿编造具体数据；用户若询问推荐或买卖方向，请给出明确观点与理由。"

	directAnswer, err := generatePersonaDirectAnswer(ctx, systemPrompt, req.Message)
	if err != nil {
		observability.L().WarnCtx(ctx, "Persona direct chat failed, using template fallback",
			"feature", "persona",
			"mode", "chat_direct",
			"persona_id", req.PersonaID,
			"error", err.Error())
		directAnswer = fmt.Sprintf("作为%s，我认为：%s。针对「%s」还需结合最新市场数据进一步判断。",
			profile.Name, profile.OneLiner, strings.TrimSpace(req.Message))
	}

	summary := strings.TrimSpace(directAnswer)
	answer := &model.PersonaAnswer{
		PersonaID:   req.PersonaID,
		PersonaName: profile.Name,
		Summary:     summary,
		Stance:      promptBuilder.BuildStance(req.Message, summary),
		Thesis:      dedupeThesisAgainstSummary(summary, promptBuilder.BuildThesis(summary)),
		Citations:   nil,
		Disclaimer:  promptBuilder.BuildDisclaimer() + "（本回答未引用系统内实时检索资料。）",
		RequestID:   requestID,
	}

	metrics.RecordPersonaChatRequest("success", req.PersonaID, time.Since(startTime).Seconds(), 0)
	observability.L().InfoCtx(ctx, "Persona chat completed (direct)",
		"feature", "persona",
		"mode", "chat_direct",
		"request_id", requestID,
		"persona_id", req.PersonaID,
		"latency_ms", time.Since(startTime).Milliseconds())

	return answer, nil
}

func generatePersonaDirectAnswer(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	client := llm.GetLLMClient()
	if client == nil {
		return "", fmt.Errorf("llm client not initialized")
	}

	llmReq := &concurrency.LLMRequest{
		RequestID: fmt.Sprintf("persona-direct-%d", time.Now().UnixNano()),
		Question:  userMessage,
		Messages: []*schema.Message{
			schema.SystemMessage(systemPrompt),
			schema.UserMessage(strings.TrimSpace(userMessage)),
		},
		TaskType: "persona_direct",
		Priority: 0,
		Timeout:  2 * time.Minute,
	}
	return client.Generate(ctx, llmReq)
}

// buildCounterViewSummary 构建反方观点摘要
func buildCounterViewSummary(profile *model.PersonaProfile, question string) string {
	if len(profile.DisagreePersonaIDs) == 0 {
		return "从相反角度看，可能存在不同的风险因素和投资观点。"
	}

	// 根据 persona 类型生成针对性的反方观点
	isGrowth := false
	isValue := false
	for _, tag := range profile.StyleTags {
		if tag == "growth" || tag == "ai" || tag == "tech" {
			isGrowth = true
		}
		if tag == "value" || tag == "dividend" {
			isValue = true
		}
	}

	if isGrowth {
		return "价值派投资者可能会认为当前估值过高，建议关注基本面和估值合理性。"
	}
	if isValue {
		return "成长派投资者可能会认为应关注企业的长期增长潜力而非短期估值。"
	}

	return "其他投资风格的投资者可能会有不同的看法和投资策略。"
}

func (s *ConfigPersonaService) Roundtable(ctx context.Context, req *model.RoundtableRequest) (*model.RoundtableResponse, error) {
	startTime := time.Now()
	requestID := fmt.Sprintf("roundtable-%d", time.Now().UnixNano())
	questionHash := hashString(req.Question)
	questionLen := len(req.Question)

	// 如果未指定 persona_ids，使用默认组合（美股成长 + A股价值）
	personaIDs := req.PersonaIDs
	if len(personaIDs) == 0 {
		personaIDs = []string{"us_growth_tech", "us_value_recovery", "cn_dividend_defensive"}
	}

	// 验证参与者数量
	if len(personaIDs) < 2 {
		metrics.RecordPersonaRoundtableRequest("invalid", 0, len(personaIDs), 0, 0, 0, 0, 0)
		observability.L().ErrorCtx(ctx, "Roundtable failed - insufficient participants", nil,
			"feature", "persona",
			"mode", "roundtable",
			"request_id", requestID,
			"participant_count", len(personaIDs),
			"question_len", questionLen,
			"question_hash", questionHash,
			"error_code", "insufficient_participants")
		return nil, fmt.Errorf("至少需要 2 个参与者")
	}
	if len(personaIDs) > 5 {
		metrics.RecordPersonaRoundtableRequest("invalid", 0, len(personaIDs), 0, 0, 0, 0, 0)
		observability.L().ErrorCtx(ctx, "Roundtable failed - too many participants", nil,
			"feature", "persona",
			"mode", "roundtable",
			"request_id", requestID,
			"participant_count", len(personaIDs),
			"question_len", questionLen,
			"question_hash", questionHash,
			"error_code", "too_many_participants")
		return nil, fmt.Errorf("最多支持 5 个参与者")
	}

	// 收集有效的 persona profiles
	validProfiles := []*model.PersonaProfile{}
	invalidIDs := []string{}
	for _, id := range personaIDs {
		profile, err := s.loader.GetPersona(id)
		if err != nil {
			invalidIDs = append(invalidIDs, id)
			continue
		}
		if profile.Status != "active" {
			invalidIDs = append(invalidIDs, id)
			continue
		}
		validProfiles = append(validProfiles, profile)
	}

	// 如果有效参与者不足，返回错误
	if len(validProfiles) < 2 {
		metrics.RecordPersonaRoundtableRequest("invalid", 0, len(validProfiles), 0, len(invalidIDs), 0, 0, 0)
		observability.L().ErrorCtx(ctx, "Roundtable failed - insufficient valid participants", nil,
			"feature", "persona",
			"mode", "roundtable",
			"request_id", requestID,
			"participant_count", len(validProfiles),
			"invalid_count", len(invalidIDs),
			"question_len", questionLen,
			"question_hash", questionHash,
			"error_code", "insufficient_valid_participants")
		return nil, fmt.Errorf("有效参与者不足，需要至少 2 个活跃的 persona")
	}

	// 记录 Roundtable 开始
	observability.L().InfoCtx(ctx, "Roundtable started",
		"feature", "persona",
		"mode", "roundtable",
		"request_id", requestID,
		"participant_count", len(validProfiles),
		"participant_ids", strings.Join(personaIDs, ","),
		"question_len", questionLen,
		"question_hash", questionHash)

	// MultiAgentCoordinator（debate 拓扑）：多轮顺序辩论（各角色可见他人发言后再回应）
	answers, debateSummary, debateSteps, debateErr := s.runRoundtableDebate(ctx, req, validProfiles, requestID)
	successCount := len(validProfiles)
	failedPersonas := []string{}
	if debateErr != nil {
		observability.L().WarnCtx(ctx, "Roundtable debate coordinator failed, using per-persona fallback",
			"feature", "persona",
			"mode", "roundtable",
			"request_id", requestID,
			"error", debateErr.Error(),
			"debate_steps", debateSteps)
		answers, failedPersonas = s.roundtableFanoutFallback(ctx, req, validProfiles, requestID)
		successCount = len(validProfiles) - len(failedPersonas)
	}

	// 构建参与者列表
	participants := make([]model.PersonaCard, len(validProfiles))
	for i, profile := range validProfiles {
		participants[i] = model.PersonaCard{
			PersonaID: profile.PersonaID,
			Name:      profile.Name,
			Market:    profile.Market,
			StyleTags: profile.StyleTags,
			OneLiner:  profile.OneLiner,
			RiskLevel: getRiskLevel(profile.Performance.MaxDrawdown1Y),
			Status:    profile.Status,
		}
	}

	// 基于所有回答生成共识、分歧和风险焦点
	consensus, disagreements, riskFocus := analyzeAnswers(answers)

	// 生成 moderation notice
	var moderationNotice string
	debateRounds := personaDebateMaxRounds()
	if len(failedPersonas) > 0 {
		moderationNotice = fmt.Sprintf("辩论协调部分失败（%d 轮 MultiAgentCoordinator，%d 步），以下参与者降级为独立回答：%s。",
			debateRounds, debateSteps, strings.Join(failedPersonas, ", "))
	} else if debateErr == nil {
		moderationNotice = fmt.Sprintf("经 %d 轮投资圆桌辩论（MultiAgentCoordinator，共 %d 步）后综合各方观点。", debateRounds, debateSteps)
		if strings.TrimSpace(debateSummary) != "" {
			moderationNotice += " 主持摘要已纳入共识分析。"
		}
	} else {
		moderationNotice = "辩论协调不可用，已降级为各角色独立回答后综合。"
	}

	// 记录指标和日志
	latencyMs := time.Since(startTime).Milliseconds()
	status := "success"
	if len(failedPersonas) > 0 {
		status = "partial_success"
	}

	metrics.RecordPersonaRoundtableRequest(
		status,
		time.Since(startTime).Seconds(),
		len(validProfiles),
		successCount,
		len(failedPersonas),
		len(consensus),
		len(disagreements),
		len(riskFocus),
	)

	observability.L().InfoCtx(ctx, "Roundtable completed",
		"feature", "persona",
		"mode", "roundtable",
		"request_id", requestID,
		"participant_count", len(validProfiles),
		"participant_ids", strings.Join(personaIDs, ","),
		"success_count", successCount,
		"failed_count", len(failedPersonas),
		"consensus_count", len(consensus),
		"disagreements_count", len(disagreements),
		"risk_focus_count", len(riskFocus),
		"question_len", questionLen,
		"question_hash", questionHash,
		"latency_ms", latencyMs,
		"status", status)

	return &model.RoundtableResponse{
		Question:         req.Question,
		Participants:     participants,
		Answers:          answers,
		Consensus:        consensus,
		Disagreements:    disagreements,
		RiskFocus:        riskFocus,
		ModerationNotice: moderationNotice,
		RequestID:        requestID,
	}, nil
}

// analyzeAnswers 分析所有 persona 的回答，提取共识、分歧和风险焦点
func analyzeAnswers(answers []model.PersonaAnswer) (consensus []string, disagreements []string, riskFocus []string) {
	if len(answers) == 0 {
		return []string{"暂无共识"}, []string{"暂无分歧"}, []string{"暂无风险焦点"}
	}

	// 收集所有论点、风险和立场
	allTheses := []string{}
	allRisks := []string{}
	allStances := []string{}
	personaIDs := []string{}

	for _, answer := range answers {
		allTheses = append(allTheses, answer.Thesis...)
		allRisks = append(allRisks, answer.Risks...)
		allStances = append(allStances, answer.Stance)
		personaIDs = append(personaIDs, answer.PersonaID)
	}

	// 提取共识：找出出现多次的论点，或者综合不同 persona 的共同观点
	consensus = extractConsensus(answers, allTheses)

	// 分析分歧：基于 persona 的风格差异和立场差异
	disagreements = extractDisagreements(answers, allStances, personaIDs)

	// 提取风险焦点：找出最常见的风险，并基于 persona 的风险偏好进行综合
	riskFocus = extractRiskFocus(allRisks)

	// 如果没有明显的共识，生成一个基于参与者风格的综合观点
	if len(consensus) == 0 {
		consensus = generateDefaultConsensus(personaIDs)
	}

	// 如果没有明显的分歧，生成一个通用分歧描述
	if len(disagreements) == 0 {
		disagreements = generateDefaultDisagreements(personaIDs)
	}

	// 如果没有明显的风险焦点，使用默认值
	if len(riskFocus) == 0 {
		riskFocus = []string{"宏观经济形势", "政策变化风险", "市场情绪波动"}
	}

	return consensus, disagreements, riskFocus
}

// extractConsensus 从多个回答中提取共识
func extractConsensus(answers []model.PersonaAnswer, allTheses []string) []string {
	consensus := []string{}

	// 统计论点出现次数
	thesisCount := make(map[string]int)
	for _, thesis := range allTheses {
		thesisCount[thesis]++
	}

	// 找出出现多次的论点作为共识
	for thesis, count := range thesisCount {
		if count >= len(answers)/2 {
			consensus = append(consensus, thesis)
		}
	}

	// 如果没有重复出现的论点，尝试从不同 persona 的回答中寻找共同主题
	if len(consensus) == 0 && len(answers) >= 2 {
		// 检查是否有共同提到的行业或主题
		commonThemes := findCommonThemes(answers)
		consensus = append(consensus, commonThemes...)
	}

	return consensus
}

// findCommonThemes 从回答中找出共同主题
func findCommonThemes(answers []model.PersonaAnswer) []string {
	themeCount := make(map[string]int)
	themes := []string{"基本面", "估值", "增长", "政策", "技术", "市场", "风险", "机会"}

	for _, answer := range answers {
		for _, thesis := range answer.Thesis {
			for _, theme := range themes {
				if strings.Contains(thesis, theme) {
					themeCount[theme]++
				}
			}
		}
	}

	common := []string{}
	for theme, count := range themeCount {
		if count >= len(answers)/2 {
			switch theme {
			case "基本面":
				common = append(common, "基本面分析是关键考量因素")
			case "估值":
				common = append(common, "估值水平受到关注")
			case "增长":
				common = append(common, "增长前景是讨论重点")
			case "政策":
				common = append(common, "政策环境影响投资决策")
			case "技术":
				common = append(common, "技术因素被重视")
			case "市场":
				common = append(common, "市场情绪值得关注")
			case "风险":
				common = append(common, "风险控制是共同关注点")
			case "机会":
				common = append(common, "市场存在结构性机会")
			}
		}
	}

	return common
}

// extractDisagreements 分析分歧点
func extractDisagreements(answers []model.PersonaAnswer, allStances []string, personaIDs []string) []string {
	if len(answers) < 2 {
		return []string{"参与方观点趋于一致"}
	}

	disagreements := []string{}

	// 检查成长派和价值派是否同时参与
	hasGrowth := false
	hasValue := false
	for _, id := range personaIDs {
		tags := getPersonaStyleTags(id)
		for _, tag := range tags {
			if tag == "growth" || tag == "ai" || tag == "tech" {
				hasGrowth = true
			}
			if tag == "value" || tag == "dividend" {
				hasValue = true
			}
		}
	}

	// 基于风格差异生成分歧描述
	if hasGrowth && hasValue {
		disagreements = append(disagreements,
			"成长派与价值派存在理念分歧：成长派更关注未来增长潜力，价值派更看重当前估值水平",
			"对估值合理性的判断存在差异",
			"投资期限和风险容忍度不同")
	}

	// 检查立场是否存在明显差异
	stanceTypes := map[string]bool{}
	for _, stance := range allStances {
		if strings.Contains(stance, "bullish") || strings.Contains(stance, "optimistic") {
			stanceTypes["bullish"] = true
		}
		if strings.Contains(stance, "bearish") || strings.Contains(stance, "cautious") {
			stanceTypes["bearish"] = true
		}
	}

	if len(stanceTypes) > 1 {
		disagreements = append(disagreements, "不同参与者对市场方向判断存在差异")
	}

	// 如果没有明显的分歧，生成通用描述
	if len(disagreements) == 0 {
		disagreements = append(disagreements, "不同投资风格存在视角差异")
	}

	return disagreements
}

// extractRiskFocus 提取风险焦点
func extractRiskFocus(allRisks []string) []string {
	if len(allRisks) == 0 {
		return []string{}
	}

	// 统计风险出现次数
	riskCount := make(map[string]int)
	for _, risk := range allRisks {
		riskCount[risk]++
	}

	// 找出出现次数最多的风险
	type riskItem struct {
		risk  string
		count int
	}
	riskList := []riskItem{}
	for risk, count := range riskCount {
		riskList = append(riskList, riskItem{risk: risk, count: count})
	}

	// 按出现次数排序
	for i := 0; i < len(riskList)-1; i++ {
		for j := i + 1; j < len(riskList); j++ {
			if riskList[j].count > riskList[i].count {
				riskList[i], riskList[j] = riskList[j], riskList[i]
			}
		}
	}

	// 取前3个风险作为焦点
	riskFocus := []string{}
	for i, item := range riskList {
		if i >= 3 {
			break
		}
		riskFocus = append(riskFocus, item.risk)
	}

	return riskFocus
}

// generateDefaultConsensus 生成默认共识
func generateDefaultConsensus(personaIDs []string) []string {
	hasUS := false
	hasCN := false
	for _, id := range personaIDs {
		if strings.HasPrefix(id, "us_") {
			hasUS = true
		}
		if strings.HasPrefix(id, "cn_") {
			hasCN = true
		}
	}

	if hasUS && hasCN {
		return []string{"跨市场分析显示存在结构性机会"}
	}
	return []string{"市场存在结构性机会"}
}

// generateDefaultDisagreements 生成默认分歧描述
func generateDefaultDisagreements(personaIDs []string) []string {
	if len(personaIDs) >= 3 {
		return []string{"多风格参与者存在观点差异"}
	}
	return []string{"不同投资风格存在视角差异"}
}

// getPersonaStyleTags 获取 persona 的风格标签（简化实现）
func getPersonaStyleTags(personaID string) []string {
	styleMap := map[string][]string{
		"us_growth_tech":        {"growth", "tech"},
		"us_value_recovery":     {"value"},
		"us_momentum":           {"momentum"},
		"cn_dividend_defensive": {"dividend", "defensive"},
		"cn_growth":             {"growth"},
		"cn_momentum":           {"momentum"},
	}
	return styleMap[personaID]
}

func getCounterPersonaID(ids []string) string {
	if len(ids) > 0 {
		return ids[0]
	}
	return ""
}

func getCounterPersonaName(ids []string, loader *config.PersonaConfigLoader) string {
	if len(ids) > 0 {
		if profile, err := loader.GetPersona(ids[0]); err == nil {
			return profile.Name
		}
	}
	return "反方观点"
}

func getRiskLevel(maxDrawdown float64) string {
	switch {
	case maxDrawdown >= -10:
		return "low"
	case maxDrawdown >= -20:
		return "medium"
	default:
		return "high"
	}
}

// hashString 生成字符串的哈希值（用于日志脱敏）
func hashString(s string) string {
	h := fnv.New64a()
	h.Write([]byte(s))
	return strconv.FormatUint(h.Sum64(), 16)
}

// checkSafety 检查问题是否包含安全敏感内容（不含正常投研/荐股类提问）。
// 返回非空字符串表示拒绝原因，空字符串表示通过检查。
func checkSafety(question string) string {
	sensitiveKeywords := []string{
		"内幕消息", "操纵市场",
		"违法", "违规", "诈骗", "骗局", "传销", "洗钱",
		"政治", "领导人", "敏感", "反动", "颠覆", "分裂",
		"色情", "暴力", "恐怖", "血腥", "毒品", "赌博",
	}

	questionLower := strings.ToLower(question)

	for _, keyword := range sensitiveKeywords {
		if strings.Contains(questionLower, keyword) {
			return "safety_policy_violation"
		}
	}

	// 检查问题长度（过短或过长可能是恶意请求）
	if len(question) < 2 {
		return "question_too_short"
	}
	if len(question) > 2000 {
		return "question_too_long"
	}

	return ""
}
