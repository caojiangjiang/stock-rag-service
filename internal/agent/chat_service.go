package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"stock_rag/internal/cache"
	einoagent "stock_rag/internal/eino/agent"
	"stock_rag/internal/memory"
	"stock_rag/internal/memory/medium"
	"stock_rag/internal/memory/short"
	"stock_rag/internal/metrics"
	"stock_rag/internal/observability"
	"stock_rag/internal/repository"
	"stock_rag/internal/router"
)

const (
	chatRecentMessageLimit = 5
	chatTitleRuneLimit     = 50
)

// chatContext 承载聊天处理流程中跨阶段共享的状态
type chatContext struct {
	ctx             context.Context
	span            trace.Span
	req             *ChatRequest
	onChunk         func(string) error
	startTime       time.Time
	convID          string
	userMsg         *repository.Message
	routeDecision   *router.RouteDecision
	recentMessages  []router.MessageContext
	summary         string
	resolvedMessage string
	confirmedFacts  []*medium.ConfirmedFact
	executeReq      *ExecuteRequest
	executeResp     *ExecuteResponse
	assistantMsg    *repository.Message
}

type ChatService struct {
	routeEngine         *router.RouteEngine
	coordinatorSelector *einoagent.CoordinatorSelector
	executor            *AgentExecutor
	conversation        repository.UnifiedConversationStore
	exactCache          *cache.ExactCache // 精确缓存（原始问题MD5匹配）
	mem                 memory.Memory     // 短/中/长期记忆
}

func NewChatService(
	routeEngine *router.RouteEngine,
	coordinatorSelector *einoagent.CoordinatorSelector,
	executor *AgentExecutor,
	conversation repository.UnifiedConversationStore,
	exactCache *cache.ExactCache,
	mem memory.Memory,
) *ChatService {
	return &ChatService{
		routeEngine:         routeEngine,
		coordinatorSelector: coordinatorSelector,
		executor:            executor,
		conversation:        conversation,
		exactCache:          exactCache,
		mem:                 mem,
	}
}

type ChatRequest struct {
	ConversationID string `json:"conversation_id"`
	UserID         string `json:"user_id"`
	Message        string `json:"message"`
	Mode           string `json:"mode,omitempty"`
	StockCode      string `json:"stock_code,omitempty"`
	DocType        string `json:"doc_type,omitempty"`
	TimeRange      string `json:"time_range,omitempty"`
}

type ChatResponse struct {
	ConversationID string        `json:"conversation_id,omitempty"`
	MessageID      string        `json:"message_id"`
	Content        string        `json:"content"`
	Mode           string        `json:"mode"`
	InputTokens    int           `json:"input_tokens"`
	OutputTokens   int           `json:"output_tokens"`
	LatencyMs      int           `json:"latency_ms"`
	Citations      []interface{} `json:"citations,omitempty"`
	Error          string        `json:"error,omitempty"`
	AwaitingHuman  bool          `json:"awaiting_human,omitempty"`
	CheckPointID   string        `json:"checkpoint_id,omitempty"`
	InterruptID    string        `json:"interrupt_id,omitempty"`
	InterruptInfo  string        `json:"interrupt_info,omitempty"`
	PartialContent string        `json:"partial_content,omitempty"`
}

// chatError 创建统一的聊天错误响应
func chatError(convID, msg string, err error) (*ChatResponse, error) {
	resp := &ChatResponse{ConversationID: convID}
	if err != nil {
		resp.Error = msg + ": " + err.Error()
	} else {
		resp.Error = msg
	}
	return resp, err
}

// chatErrorResponse 创建只返回响应的错误（用于不需要返回 error 的场景）
func chatErrorResponse(convID, msg string) *ChatResponse {
	return &ChatResponse{
		ConversationID: convID,
		Error:          msg,
	}
}

func (s *ChatService) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	ctx, span := observability.StartSpan(ctx, "ChatService.Chat")
	defer span.End()
	return s.chat(ctx, req, nil)
}

func (s *ChatService) ChatStream(ctx context.Context, req *ChatRequest, onChunk func(string) error) (*ChatResponse, error) {
	ctx, span := observability.StartSpan(ctx, "ChatService.ChatStream")
	defer span.End()
	return s.chat(ctx, req, onChunk)
}

func (s *ChatService) chat(ctx context.Context, req *ChatRequest, onChunk func(string) error) (resp *ChatResponse, err error) {
	ctx, span := observability.StartSpan(ctx, "ChatService.chat")
	defer span.End()

	cc := &chatContext{
		ctx:       ctx,
		span:      span,
		req:       req,
		onChunk:   onChunk,
		startTime: time.Now(),
	}

	defer func() {
		status := "success"
		mode := "unknown"
		if resp != nil {
			if resp.Mode != "" {
				mode = resp.Mode
			}
			if resp.Error != "" {
				status = "error"
			} else if resp.AwaitingHuman {
				status = "awaiting_human"
			}
		}
		if err != nil {
			status = "error"
		}
		metrics.RecordChatRequest(mode, status, time.Since(cc.startTime).Seconds())
	}()

	if resp, err := s.validate(cc); err != nil {
		return resp, err
	}
	if resp, err := s.initConversationContext(cc); err != nil {
		return resp, err
	}
	if resp, err := s.route(cc); err != nil {
		return resp, err
	}
	if resp, err := s.persistUserMessage(cc); err != nil {
		return resp, err
	}
	if resp := s.checkExactCache(cc); resp != nil {
		return resp, nil
	}
	if resp, err := s.loadMemory(cc); err != nil {
		return resp, err
	}
	if resp, err := s.execute(cc); err != nil {
		return resp, err
	}
	if err := s.saveAssistantMessage(cc); err != nil {
		return chatError(cc.convID, "保存助手消息失败", err)
	}

	return s.buildResponse(cc), nil
}

// validate 验证请求参数
func (s *ChatService) validate(cc *chatContext) (*ChatResponse, error) {
	if cc.req == nil {
		return chatError("", "chat request is nil", fmt.Errorf("chat request is nil"))
	}

	cc.req.Message = strings.TrimSpace(cc.req.Message)
	if cc.req.Mode != "" {
		cc.span.SetAttributes(attribute.String("chat.explicit_mode", cc.req.Mode))
		observability.L().InfoCtx(cc.ctx, "Explicit mode specified", "mode", cc.req.Mode)
	}

	cc.span.SetAttributes(attribute.String("chat.conversation_id", cc.req.ConversationID))
	return nil, nil
}

// initConversationContext 初始化会话上下文：生成会话ID、获取历史消息、生成摘要
func (s *ChatService) initConversationContext(cc *chatContext) (*ChatResponse, error) {
	cc.convID = strings.TrimSpace(cc.req.ConversationID)
	if cc.convID == "" {
		cc.convID = newConversationID()
		observability.L().InfoCtx(cc.ctx, "Generated new conversation ID", "conversation_id", cc.convID)
	}

	observability.L().InfoCtx(cc.ctx, "Starting chat request",
		"conversation_id", cc.convID,
		"user_id", cc.req.UserID,
		"message_preview", truncateMessageForLog(cc.req.Message, 120),
	)

	var err error
	cc.recentMessages, err = s.getRecentMessages(cc.ctx, cc.convID, chatRecentMessageLimit)
	if err != nil {
		observability.L().ErrorCtx(cc.ctx, "Failed to load recent messages", err)
		cc.recentMessages = []router.MessageContext{}
	}

	cc.summary, err = s.getConversationSummary(cc.ctx, cc.convID)
	if err != nil {
		observability.L().ErrorCtx(cc.ctx, "Failed to load conversation summary", err)
		cc.summary = ""
	}

	cc.userMsg = repository.NewMessage(cc.convID, cc.req.UserID, "user", cc.req.Message, nil)
	return nil, nil
}

// route 执行路由决策
func (s *ChatService) route(cc *chatContext) (*ChatResponse, error) {
	lastRouteMode := deriveLastRouteMode(cc.recentMessages)
	if lastRouteMode == "" {
		lastRouteMode = s.getLastRouteMode(cc.ctx, cc.convID)
	}

	var explicitMode router.RouteMode
	if cc.req.Mode != "" {
		explicitMode = router.RouteMode(cc.req.Mode)
	}

	routeInput := &router.RouteInput{
		MessageID:      cc.userMsg.ID,
		ConversationID: cc.convID,
		UserID:         cc.req.UserID,
		CurrentMessage: cc.req.Message,
		RecentMessages: cc.recentMessages,
		Summary:        cc.summary,
		LastRouteMode:  lastRouteMode,
		ExplicitMode:   explicitMode,
		StockCode:      cc.req.StockCode,
		DocType:        cc.req.DocType,
		TimeRange:      cc.req.TimeRange,
	}

	observability.L().InfoCtx(cc.ctx, "Starting route decision")
	var err error
	cc.routeDecision, err = s.routeEngine.Route(cc.ctx, routeInput)
	if err != nil {
		return chatError(cc.convID, "路由失败", err)
	}

	observability.L().InfoCtx(cc.ctx, "Route decision completed",
		"selected_mode", string(cc.routeDecision.SelectedMode),
		"message_id", cc.routeDecision.MessageID,
	)

	return nil, nil
}

// persistUserMessage 持久化用户消息
func (s *ChatService) persistUserMessage(cc *chatContext) (*ChatResponse, error) {
	cc.userMsg.RouteMode = string(cc.routeDecision.SelectedMode)

	if err := s.ensureConversationExists(cc.ctx, cc.convID, cc.req.UserID, cc.req.Message); err != nil {
		observability.L().ErrorCtx(cc.ctx, "Failed to ensure conversation exists", err)
		return chatError(cc.convID, "创建会话失败", err)
	}

	if err := s.conversation.SaveMessage(cc.ctx, cc.userMsg); err != nil {
		observability.L().ErrorCtx(cc.ctx, "Failed to save user message", err)
		return chatError(cc.convID, "保存用户消息失败", err)
	}

	observability.L().InfoCtx(cc.ctx, "User message saved",
		"conversation_id", cc.convID,
		"message_id", cc.userMsg.ID,
	)

	return nil, nil
}

// loadMemory 加载记忆：短期记忆（指代消解）和中期记忆（已确认事实）
func (s *ChatService) loadMemory(cc *chatContext) (*ChatResponse, error) {
	cc.resolvedMessage = s.loadShortTermMemory(cc.ctx, cc.convID, cc.req.Message)
	cc.confirmedFacts = s.loadMediumTermMemory(cc.ctx, cc.convID, cc.req.StockCode)
	return nil, nil
}

// execute 执行请求并处理响应
func (s *ChatService) execute(cc *chatContext) (*ChatResponse, error) {
	cc.executeReq = s.buildExecuteRequest(
		cc.ctx, cc.convID, cc.req, cc.userMsg, cc.routeDecision,
		cc.resolvedMessage, cc.confirmedFacts, cc.recentMessages, cc.summary, cc.onChunk,
	)
	if cc.executeReq == nil {
		return chatError(cc.convID, "协调器选择失败", fmt.Errorf("coordinator selection failed"))
	}

	observability.L().InfoCtx(cc.ctx, "Executing request",
		"conversation_id", cc.convID,
		"message_id", cc.userMsg.ID,
		"mode", string(cc.routeDecision.SelectedMode),
	)

	if cc.routeDecision.SelectedMode == router.ModeAgent {
		_ = repository.SetConversationStatus(cc.ctx, s.conversation, cc.convID, repository.ConversationStatusRunning)
	}

	var err error
	cc.executeResp, err = s.executor.Execute(cc.ctx, cc.executeReq)
	if err != nil {
		observability.L().ErrorCtx(cc.ctx, "Execution failed", err)
		return chatError(cc.convID, "执行失败", err)
	}
	if cc.executeResp == nil {
		return chatError(cc.convID, "执行失败: executor returned nil response", fmt.Errorf("executor returned nil response"))
	}

	if cc.executeResp.Error != "" {
		observability.L().ErrorCtx(cc.ctx, "Execution returned error", nil, "error", cc.executeResp.Error)
		_ = repository.SetConversationStatus(cc.ctx, s.conversation, cc.convID, repository.ConversationStatusActive)
		return chatError(cc.convID, cc.executeResp.Error, nil)
	}

	if cc.executeResp.AwaitingHuman {
		_ = repository.SetConversationStatus(cc.ctx, s.conversation, cc.convID, repository.ConversationStatusPendingHuman)
		observability.L().InfoCtx(cc.ctx, "Execution awaiting human",
			"conversation_id", cc.convID,
			"checkpoint_id", cc.executeResp.CheckPointID,
			"interrupt_id", cc.executeResp.InterruptID,
		)
	} else if cc.routeDecision.SelectedMode == router.ModeAgent {
		_ = repository.SetConversationStatus(cc.ctx, s.conversation, cc.convID, repository.ConversationStatusActive)
	}

	if cc.onChunk != nil && cc.routeDecision.SelectedMode == router.ModeAgent && !cc.executeResp.AwaitingHuman {
		if err := EmitStreamChunks(cc.onChunk, cc.executeResp.Content); err != nil {
			return chatError(cc.convID, "流式输出失败", err)
		}
	}

	observability.L().InfoCtx(cc.ctx, "Execution completed",
		"message_id", cc.executeResp.MessageID,
		"content_length", len(cc.executeResp.Content),
		"input_tokens", cc.executeResp.InputTokens,
		"output_tokens", cc.executeResp.OutputTokens,
	)

	return nil, nil
}

// saveAssistantMessage 保存助手消息
func (s *ChatService) saveAssistantMessage(cc *chatContext) error {
	content := cc.executeResp.Content
	if cc.executeResp.AwaitingHuman && cc.executeResp.PartialContent != "" {
		content = cc.executeResp.PartialContent
	}
	var metadata map[string]interface{}
	if cc.executeResp.AwaitingHuman {
		metadata = map[string]interface{}{
			"awaiting_human": true,
			"checkpoint_id":  cc.executeResp.CheckPointID,
			"interrupt_id":   cc.executeResp.InterruptID,
			"interrupt_info": cc.executeResp.InterruptInfo,
		}
	}
	cc.assistantMsg = repository.NewMessage(cc.convID, cc.req.UserID, "assistant", content, metadata)
	cc.assistantMsg.RouteMode = string(cc.routeDecision.SelectedMode)
	if cc.routeDecision.SelectedMode == router.ModeAgent && cc.executeReq.CoordinatorType != "" {
		cc.assistantMsg.CoordinatorType = cc.executeReq.CoordinatorType
		repository.ApplyCoordinatorMetadata(cc.assistantMsg)
	}

	if err := s.conversation.SaveMessage(cc.ctx, cc.assistantMsg); err != nil {
		return err
	}

	observability.L().InfoCtx(cc.ctx, "Assistant message saved",
		"conversation_id", cc.convID,
		"message_id", cc.assistantMsg.ID,
	)

	cc.executeResp.MessageID = cc.assistantMsg.ID
	s.maybeCacheExactResponse(cc)
	return nil
}

// buildResponse 构建最终响应
func (s *ChatService) buildResponse(cc *chatContext) *ChatResponse {
	latency := int(time.Since(cc.startTime).Milliseconds())
	observability.L().InfoCtx(cc.ctx, "Chat request completed", "latency_ms", latency)

	content := cc.executeResp.Content
	if cc.executeResp.AwaitingHuman && cc.executeResp.PartialContent != "" {
		content = cc.executeResp.PartialContent
	}
	return &ChatResponse{
		ConversationID: cc.convID,
		MessageID:      cc.executeResp.MessageID,
		Content:        content,
		Mode:           string(cc.executeResp.Mode),
		InputTokens:    cc.executeResp.InputTokens,
		OutputTokens:   cc.executeResp.OutputTokens,
		LatencyMs:      latency,
		Citations:      s.citationsToInterface(cc.executeResp.Citations),
		AwaitingHuman:  cc.executeResp.AwaitingHuman,
		CheckPointID:   cc.executeResp.CheckPointID,
		InterruptID:    cc.executeResp.InterruptID,
		InterruptInfo:  cc.executeResp.InterruptInfo,
		PartialContent: cc.executeResp.PartialContent,
	}
}

func (s *ChatService) getRecentMessages(ctx context.Context, convID string, count int) ([]router.MessageContext, error) {
	if convID == "" {
		return []router.MessageContext{}, nil
	}

	messages, err := s.conversation.GetMessages(ctx, convID, count)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return []router.MessageContext{}, nil
	}

	result := make([]router.MessageContext, 0, len(messages))
	for _, msg := range messages {
		result = append(result, router.MessageContext{
			Role:            msg.Role,
			Content:         msg.Content,
			RouteMode:       router.RouteMode(msg.RouteMode),
			CoordinatorType: repository.CoordinatorTypeFromMessage(msg),
			CreatedAt:       time.Unix(msg.CreatedAt, 0),
		})
	}
	return result, nil
}

func (s *ChatService) getConversationSummary(ctx context.Context, convID string) (string, error) {
	if convID == "" {
		return "", nil
	}

	summary, err := s.conversation.GetSummary(ctx, convID)
	if err != nil {
		if err == repository.ErrNotFound {
			return "", nil
		}
		return "", err
	}
	// 生成摘要文本
	var summaryText string
	if summary.CurrentObject != "" {
		summaryText += "当前对象: " + summary.CurrentObject + "; "
	}
	if summary.TimeRange != "" {
		summaryText += "时间范围: " + summary.TimeRange + "; "
	}
	if len(summary.DocTypes) > 0 {
		summaryText += "文档类型: " + summary.DocTypes[0]
	}
	return summaryText, nil
}

func (s *ChatService) getLastRouteMode(ctx context.Context, convID string) router.RouteMode {
	if convID == "" {
		return ""
	}

	routeMode, err := s.conversation.GetLastRouteMode(ctx, convID)
	if err != nil || routeMode == "" {
		return ""
	}
	return router.RouteMode(routeMode)
}

func (s *ChatService) ensureConversationExists(ctx context.Context, conversationID, userID, title string) error {
	if conversationID == "" {
		return nil
	}

	_, err := s.conversation.GetConversation(ctx, conversationID)
	if err == nil {
		return nil
	}
	if err != repository.ErrNotFound {
		return err
	}

	conversation := repository.NewConversation(conversationID, userID, truncateConversationTitle(title))
	return s.conversation.SaveConversation(ctx, conversation)
}

// resolvePronouns 执行指代消解，用最近提及的实体替换消息中的指代词
func resolvePronouns(message string, recentEntities []*short.EntityReference) string {
	if len(recentEntities) == 0 {
		return message
	}

	pronouns := []string{"它", "他", "她", "这", "那", "该", "其", "此"}
	resolved := message

	for _, pronoun := range pronouns {
		pattern := regexp.MustCompile(regexp.QuoteMeta(pronoun))
		if pattern.MatchString(resolved) {
			resolved = pattern.ReplaceAllString(resolved, recentEntities[0].Entity)
			break
		}
	}

	return resolved
}

// checkExactCache 检查精确缓存，返回缓存响应或 nil（未命中）
func (s *ChatService) checkExactCache(cc *chatContext) *ChatResponse {
	if s.exactCache == nil {
		return nil
	}

	cacheKey := s.exactCacheKey(cc.req)
	cacheResult, err := s.exactCache.Get(cc.ctx, cacheKey)
	if err != nil {
		observability.L().WarnCtx(cc.ctx, "Exact cache query failed", "error", err)
		return nil
	}

	if !cacheResult.Hit {
		metrics.RecordCacheMiss("exact")
		observability.L().InfoCtx(cc.ctx, "Exact cache miss, proceeding with execution",
			"cache_key", truncateString(cacheKey, 50),
		)
		return nil
	}

	if !isCacheableChatResponse(cacheResult.Response) {
		metrics.RecordCacheMiss("exact")
		observability.L().WarnCtx(cc.ctx, "Exact cache poison entry ignored, deleting",
			"cache_key", truncateString(cacheKey, 50),
		)
		go func(key string) {
			_ = s.exactCache.Delete(context.Background(), key)
		}(cacheKey)
		return nil
	}

	metrics.RecordCacheHit("exact")
	observability.L().InfoCtx(cc.ctx, "Exact cache hit, returning cached response",
		"message_id", cc.routeDecision.MessageID,
		"access_count", cacheResult.AccessCount,
		"cache_key", truncateString(cacheKey, 50),
	)

	if cc.onChunk != nil {
		if err := EmitStreamChunks(cc.onChunk, cacheResult.Response); err != nil {
			return chatErrorResponse(cc.convID, "流式输出失败: "+err.Error())
		}
	}

	assistantMsg := repository.NewMessage(cc.convID, cc.req.UserID, "assistant", cacheResult.Response, nil)
	assistantMsg.RouteMode = string(cc.routeDecision.SelectedMode)
	if err := s.conversation.SaveMessage(cc.ctx, assistantMsg); err != nil {
		observability.L().WarnCtx(cc.ctx, "Exact cache assistant save failed", "error", err)
	}

	latency := int(time.Since(cc.startTime).Milliseconds())
	return &ChatResponse{
		ConversationID: cc.convID,
		MessageID:      assistantMsg.ID,
		Content:        cacheResult.Response,
		Mode:           string(cc.routeDecision.SelectedMode),
		InputTokens:    0,
		OutputTokens:   0,
		LatencyMs:      latency,
		Citations:      nil,
	}
}

// loadShortTermMemory 加载短期记忆：任务状态和指代消解
func (s *ChatService) loadShortTermMemory(ctx context.Context, convID, message string) string {
	if s.mem == nil || s.mem.Short() == nil {
		return ""
	}

	// 查询当前任务状态
	taskState, err := s.mem.Short().GetTaskState(ctx, convID)
	if err == nil && taskState != nil {
		observability.L().InfoCtx(ctx, "Loaded task state from working memory",
			"goal", taskState.Goal,
			"status", taskState.Status,
		)
	}

	// 获取最近提及的实体（用于指代消解）
	recentEntities, err := s.mem.Short().GetRecentEntities(ctx, convID, 5)
	if err != nil || len(recentEntities) == 0 {
		return ""
	}

	// 执行指代消解：用最近的实体替换消息中的指代词
	resolvedMessage := resolvePronouns(message, recentEntities)
	if resolvedMessage != message {
		observability.L().InfoCtx(ctx, "Resolved pronouns in message",
			"original", message,
			"resolved", resolvedMessage,
		)
	}

	return resolvedMessage
}

// buildExecuteRequest 根据执行模式构建请求，处理 ModeAgent 和其他模式的差异
func (s *ChatService) buildExecuteRequest(
	ctx context.Context,
	convID string,
	req *ChatRequest,
	userMsg *repository.Message,
	routeDecision *router.RouteDecision,
	resolvedMessage string,
	confirmedFacts []*medium.ConfirmedFact,
	recentMessages []router.MessageContext,
	summary string,
	onChunk func(string) error,
) *ExecuteRequest {
	req2 := &ExecuteRequest{
		ConversationID:  convID,
		MessageID:       userMsg.ID,
		UserID:          req.UserID,
		UserMessage:     req.Message,
		ResolvedMessage: resolvedMessage,
		Mode:            routeDecision.SelectedMode,
		RouteConfidence: routeDecision.Confidence,
		RouteReason:     routeDecision.Reason,
		StockCode:       req.StockCode,
		DocType:         req.DocType,
		TimeRange:       req.TimeRange,
		ConfirmedFacts:  confirmedFacts,
	}

	switch routeDecision.SelectedMode {
	case router.ModeAgent:
		// Agent 模式：多步骤智能体，支持复杂任务编排，不支持流式输出
		coordType, coordReason, err := s.selectCoordinator(ctx, convID, req, routeDecision, recentMessages, summary)
		if err != nil {
			observability.L().ErrorCtx(ctx, "Coordinator selection failed", err)
			return nil
		}
		req2.CoordinatorType = string(coordType)
		observability.L().InfoCtx(ctx, "Coordinator selected",
			"type", coordType,
			"reason", coordReason,
		)
	default:
		// Chat 模式：直接对话，支持流式输出
		req2.OnChunk = onChunk
	}

	return req2
}

// loadMediumTermMemory 加载中期记忆：已确认事实
func (s *ChatService) loadMediumTermMemory(ctx context.Context, convID, stockCode string) []*medium.ConfirmedFact {
	if s.mem == nil || s.mem.Medium() == nil {
		return nil
	}

	sessionCtx, err := s.mem.Medium().Get(ctx, convID)
	if err != nil || sessionCtx == nil {
		return nil
	}

	// 检查是否有待验证的事实
	pendingFacts, _ := s.mem.Medium().GetPendingFacts(ctx, convID)
	if len(pendingFacts) > 0 {
		observability.L().InfoCtx(ctx, "Found pending facts in session context",
			"count", len(pendingFacts),
		)
	}

	// 如果有请求的股票代码，获取相关事实
	if stockCode == "" {
		return nil
	}

	entityFacts, _ := s.mem.Medium().GetFactsByEntity(ctx, convID, stockCode)
	if len(entityFacts) == 0 {
		return nil
	}

	var confirmedFacts []*medium.ConfirmedFact
	for key, fact := range entityFacts {
		confirmedFacts = append(confirmedFacts, fact)
		observability.L().InfoCtx(ctx, "Found confirmed fact",
			"key", key,
			"value", fmt.Sprintf("%v", fact.Value),
		)
	}

	return confirmedFacts
}

func deriveLastRouteMode(messages []router.MessageContext) router.RouteMode {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].RouteMode != "" {
			return messages[i].RouteMode
		}
	}
	return ""
}

func (s *ChatService) selectCoordinator(
	ctx context.Context,
	convID string,
	req *ChatRequest,
	routeDecision *router.RouteDecision,
	recentMessages []router.MessageContext,
	summary string,
) (einoagent.CoordinatorType, string, error) {
	if s.coordinatorSelector == nil {
		return einoagent.CoordinatorTypeSupervisor, "coordinator selector not configured", nil
	}

	lastCoordinator := deriveLastCoordinator(recentMessages)
	if lastCoordinator == "" {
		lastCoordinator = s.getLastCoordinator(ctx, convID)
	}

	selectInput := &einoagent.CoordinatorSelectInput{
		MessageID:           routeDecision.MessageID,
		ConversationID:      convID,
		CurrentMessage:      req.Message,
		RecentMessages:      recentMessages,
		Summary:             summary,
		RouteMode:           routeDecision.SelectedMode,
		RouteConfidence:     routeDecision.Confidence,
		RouteReason:         routeDecision.Reason,
		ExplicitCoordinator: ExplicitCoordinatorFromEnv(),
		LastCoordinator:     lastCoordinator,
		StockCode:           req.StockCode,
		DocType:             req.DocType,
	}

	decision, err := s.coordinatorSelector.Select(ctx, selectInput)
	if err != nil {
		return "", "", err
	}
	return decision.SelectedType, decision.Reason, nil
}

func (s *ChatService) getLastCoordinator(ctx context.Context, convID string) einoagent.CoordinatorType {
	if convID == "" {
		return ""
	}
	coord, err := s.conversation.GetLastCoordinator(ctx, convID)
	if err != nil || coord == "" {
		return ""
	}
	return einoagent.CoordinatorType(coord)
}

func deriveLastCoordinator(messages []router.MessageContext) einoagent.CoordinatorType {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].CoordinatorType != "" {
			return einoagent.CoordinatorType(messages[i].CoordinatorType)
		}
	}
	return ""
}

func newConversationID() string {
	return fmt.Sprintf("conversation-%d", time.Now().UnixNano())
}

func truncateConversationTitle(message string) string {
	runes := []rune(strings.TrimSpace(message))
	if len(runes) > chatTitleRuneLimit {
		return string(runes[:chatTitleRuneLimit])
	}
	return string(runes)
}

func truncateMessageForLog(message string, maxLen int) string {
	runes := []rune(strings.TrimSpace(message))
	if len(runes) <= maxLen {
		return string(runes)
	}
	return string(runes[:maxLen]) + "..."
}

func (s *ChatService) citationsToInterface(citations []Citation) []interface{} {
	if citations == nil {
		return nil
	}
	result := make([]interface{}, len(citations))
	for i, c := range citations {
		result[i] = map[string]interface{}{
			"stock_code": c.StockCode,
			"doc_type":   c.DocType,
			"title":      c.Title,
			"content":    c.Content,
			"score":      c.Score,
		}
	}
	return result
}

// buildExactCacheKey 构建精确缓存的复合键（不含 route mode，相同问题共享缓存）。
func buildExactCacheKey(message, stockCode, docType, timeRange string) string {
	return message + "|" + stockCode + "|" + docType + "|" + timeRange
}

// truncateString 截断字符串用于日志
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
