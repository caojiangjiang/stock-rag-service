package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"stock_rag/internal/cache"
	"stock_rag/internal/memory"
	"stock_rag/internal/memory/medium"
	"stock_rag/internal/metrics"
	"stock_rag/internal/observability"
	"stock_rag/internal/repository"
	"stock_rag/internal/router"
)

const (
	chatRecentMessageLimit = 5
	chatTitleRuneLimit     = 50
)

type ChatService struct {
	routeEngine    *router.RouteEngine
	executor       *AgentExecutor
	conversation repository.UnifiedConversationStore
	exactCache     *cache.ExactCache // 精确缓存（原始问题MD5匹配）
	mem            memory.Memory     // 短/中/长期记忆
}

func NewChatService(
	routeEngine *router.RouteEngine,
	executor *AgentExecutor,
	conversation repository.UnifiedConversationStore,
	exactCache *cache.ExactCache,
	mem memory.Memory,
) *ChatService {
	return &ChatService{
		routeEngine:  routeEngine,
		executor:     executor,
		conversation: conversation,
		exactCache:   exactCache,
		mem:          mem,
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
	ctx, span := observability.StartSpan(ctx, "ChatService.processChat")
	defer span.End()

	startTime := time.Now()
	defer func() {
		status := "success"
		mode := "unknown"
		if resp != nil {
			if resp.Mode != "" {
				mode = resp.Mode
			}
			if resp.Error != "" {
				status = "error"
			}
		}
		if err != nil {
			status = "error"
		}
		metrics.RecordChatRequest(mode, status, time.Since(startTime).Seconds())
	}()

	if req == nil {
		err := fmt.Errorf("chat request is nil")
		return &ChatResponse{Error: err.Error()}, err
	}
	if req.Mode != "" {
		span.SetAttributes(attribute.String("chat.explicit_mode", req.Mode))
	}
	span.SetAttributes(attribute.String("chat.conversation_id", req.ConversationID))

	req.Message = strings.TrimSpace(req.Message)
	convID := strings.TrimSpace(req.ConversationID)
	if convID == "" {
		convID = newConversationID()
		observability.L().InfoCtx(ctx, "Generated new conversation ID", "conversation_id", convID)
	}

	observability.L().InfoCtx(ctx, "Starting chat request",
		"conversation_id", convID,
		"user_id", req.UserID,
		"message_preview", truncateMessageForLog(req.Message, 120),
	)

	var explicitMode router.RouteMode
	if req.Mode != "" {
		explicitMode = router.RouteMode(req.Mode)
		observability.L().InfoCtx(ctx, "Explicit mode specified", "mode", string(explicitMode))
	}

	observability.L().InfoCtx(ctx, "Fetching recent messages and conversation context")
	recentMessages, err := s.getRecentMessages(ctx, convID, chatRecentMessageLimit)
	if err != nil {
		observability.L().ErrorCtx(ctx, "Failed to load recent messages", err)
		recentMessages = []router.MessageContext{}
	}
	observability.L().InfoCtx(ctx, "Found recent messages", "count", len(recentMessages))

	summary, err := s.getConversationSummary(ctx, convID)
	if err != nil {
		observability.L().ErrorCtx(ctx, "Failed to load conversation summary", err)
		summary = ""
	}

	lastRouteMode := deriveLastRouteMode(recentMessages)
	if lastRouteMode == "" {
		lastRouteMode = s.getLastRouteMode(ctx, convID)
	}
	observability.L().InfoCtx(ctx, "Last route mode", "mode", string(lastRouteMode))

	userMsg := repository.NewMessage(convID, req.UserID, "user", req.Message, nil)

	routeInput := &router.RouteInput{
		MessageID:      userMsg.ID,
		ConversationID: convID,
		UserID:         req.UserID,
		CurrentMessage: req.Message,
		RecentMessages: recentMessages,
		Summary:        summary,
		LastRouteMode:  lastRouteMode,
		ExplicitMode:   explicitMode,
		StockCode:      req.StockCode,
		DocType:        req.DocType,
		TimeRange:      req.TimeRange,
	}

	observability.L().InfoCtx(ctx, "Starting route decision")
	routeDecision, err := s.routeEngine.Route(ctx, routeInput)
	if err != nil {
		observability.L().ErrorCtx(ctx, "Route decision failed", err)
		return &ChatResponse{
			ConversationID: convID,
			Error:          "路由失败: " + err.Error(),
		}, err
	}
	observability.L().InfoCtx(ctx, "Route decision completed",
		"selected_mode", string(routeDecision.SelectedMode),
		"message_id", routeDecision.MessageID,
	)

	// 精确缓存查询（在路由决策之后执行之前）
	// 使用原始问题 + 路由模式 + 股票代码 + 文档类型 + 时间范围 作为缓存键
	if s.exactCache != nil {
		cacheKey := buildExactCacheKey(req.Message, string(routeDecision.SelectedMode), req.StockCode, req.DocType, req.TimeRange)
		cacheResult, err := s.exactCache.Get(ctx, cacheKey)
		if err != nil {
			observability.L().WarnCtx(ctx, "Exact cache query failed", "error", err)
		} else if cacheResult.Hit {
			metrics.RecordCacheHit("exact")
			observability.L().InfoCtx(ctx, "Exact cache hit, returning cached response",
				"message_id", routeDecision.MessageID,
				"access_count", cacheResult.AccessCount,
			)
			if onChunk != nil {
				if err := EmitStreamChunks(onChunk, cacheResult.Response); err != nil {
					return &ChatResponse{
						ConversationID: convID,
						Error:          "流式输出失败: " + err.Error(),
					}, err
				}
			}
			latency := int(time.Since(startTime).Milliseconds())
			return &ChatResponse{
				ConversationID: convID,
				MessageID:      routeDecision.MessageID,
				Content:        cacheResult.Response,
				Mode:           string(routeDecision.SelectedMode),
				InputTokens:    0,
				OutputTokens:   0,
				LatencyMs:      latency,
				Citations:      nil,
			}, nil
		}
		metrics.RecordCacheMiss("exact")
		observability.L().InfoCtx(ctx, "Exact cache miss, proceeding with execution")
	}

	userMsg.RouteMode = string(routeDecision.SelectedMode)

	if err := s.ensureConversationExists(ctx, convID, req.UserID, req.Message); err != nil {
		observability.L().ErrorCtx(ctx, "Failed to ensure conversation exists", err)
		return &ChatResponse{
			ConversationID: convID,
			Error:          "创建会话失败: " + err.Error(),
		}, err
	}

	if err := s.conversation.SaveMessage(ctx, userMsg); err != nil {
		observability.L().ErrorCtx(ctx, "Failed to save user message", err)
		return &ChatResponse{
			ConversationID: convID,
			Error:          "保存用户消息失败: " + err.Error(),
		}, err
	}
	observability.L().InfoCtx(ctx, "User message saved",
		"conversation_id", convID,
		"message_id", userMsg.ID,
	)

	// ========== 记忆操作 ==========
	// 1. 查询短期记忆获取 TaskState 和实体引用
	if s.mem != nil && s.mem.Short() != nil {
		// 查询当前任务状态
		taskState, err := s.mem.Short().GetTaskState(ctx, convID)
		if err == nil && taskState != nil {
			observability.L().InfoCtx(ctx, "Loaded task state from working memory",
				"goal", taskState.Goal,
				"status", taskState.Status,
			)
			span.SetAttributes(attribute.String("memory.task_state.goal", taskState.Goal))
		}

		// 解析指代（如"它"、"那家"等）
		referencedEntity, err := s.mem.Short().ResolveReference(ctx, convID, req.Message)
		if err == nil && referencedEntity != "" {
			observability.L().InfoCtx(ctx, "Resolved reference from working memory",
				"entity", referencedEntity,
			)
			span.SetAttributes(attribute.String("memory.resolved_entity", referencedEntity))
		}
	}

	// 2. 查询中期记忆获取已确认事实（避免重复检索）
	var confirmedFacts []*medium.ConfirmedFact
	if s.mem != nil && s.mem.Medium() != nil {
		sessionCtx, err := s.mem.Medium().Get(ctx, convID)
		if err == nil && sessionCtx != nil {
			// 检查是否有待验证的事实
			pendingFacts, _ := s.mem.Medium().GetPendingFacts(ctx, convID)
			if len(pendingFacts) > 0 {
				observability.L().InfoCtx(ctx, "Found pending facts in session context",
					"count", len(pendingFacts),
				)
			}

			// 如果有请求的股票代码，获取相关事实
			if req.StockCode != "" {
				entityFacts, _ := s.mem.Medium().GetFactsByEntity(ctx, convID, req.StockCode)
				for key, fact := range entityFacts {
					confirmedFacts = append(confirmedFacts, fact)
					observability.L().InfoCtx(ctx, "Found confirmed fact",
						"key", key,
						"value", fmt.Sprintf("%v", fact.Value),
					)
				}
			}
		}
	}

	executeReq := &ExecuteRequest{
		ConversationID: convID,
		MessageID:      userMsg.ID,
		UserID:         req.UserID,
		UserMessage:    req.Message,
		Mode:           routeDecision.SelectedMode,
		StockCode:      req.StockCode,
		DocType:        req.DocType,
		TimeRange:      req.TimeRange,
	}
	if onChunk != nil && routeDecision.SelectedMode != router.ModeAgent {
		executeReq.OnChunk = onChunk
	}

	observability.L().InfoCtx(ctx, "Executing request",
		"conversation_id", convID,
		"message_id", userMsg.ID,
		"mode", string(routeDecision.SelectedMode),
	)
	executeResp, err := s.executor.Execute(ctx, executeReq)
	if err != nil {
		observability.L().ErrorCtx(ctx, "Execution failed", err)
		return &ChatResponse{
			ConversationID: convID,
			Error:          "执行失败: " + err.Error(),
		}, err
	}
	if executeResp == nil {
		err := fmt.Errorf("executor returned nil response")
		return &ChatResponse{
			ConversationID: convID,
			Error:          "执行失败: " + err.Error(),
		}, err
	}

	if executeResp.Error != "" {
		observability.L().ErrorCtx(ctx, "Execution returned error", nil, "error", executeResp.Error)
		return &ChatResponse{
			ConversationID: convID,
			Error:          executeResp.Error,
		}, nil
	}

	if onChunk != nil && routeDecision.SelectedMode == router.ModeAgent {
		if err := EmitStreamChunks(onChunk, executeResp.Content); err != nil {
			return &ChatResponse{
				ConversationID: convID,
				Error:          "流式输出失败: " + err.Error(),
			}, err
		}
	}

	observability.L().InfoCtx(ctx, "Execution completed",
		"message_id", executeResp.MessageID,
		"content_length", len(executeResp.Content),
		"input_tokens", executeResp.InputTokens,
		"output_tokens", executeResp.OutputTokens,
	)

	// 执行成功后写入精确缓存（异步，避免阻塞响应）
	if s.exactCache != nil {
		cacheKey := buildExactCacheKey(req.Message, string(routeDecision.SelectedMode), req.StockCode, req.DocType, req.TimeRange)
		go func() {
			if err := s.exactCache.Set(context.Background(), cacheKey, executeResp.Content); err != nil {
				observability.L().WarnCtx(context.Background(), "Exact cache set failed", "error", err)
			} else {
				observability.L().InfoCtx(context.Background(), "Exact cache entry added",
					"cache_key", truncateString(cacheKey, 50),
				)
			}
		}()
	}

	assistantMsg := repository.NewMessage(convID, req.UserID, "assistant", executeResp.Content, nil)
	if executeResp.MessageID != "" {
		assistantMsg.ID = executeResp.MessageID
	}
	assistantMsg.RouteMode = string(routeDecision.SelectedMode)
	if err := s.conversation.SaveMessage(ctx, assistantMsg); err != nil {
		observability.L().ErrorCtx(ctx, "Failed to save assistant message", err)
		return &ChatResponse{
			ConversationID: convID,
			Error:          "保存助手消息失败: " + err.Error(),
		}, err
	}
	observability.L().InfoCtx(ctx, "Assistant message saved",
		"conversation_id", convID,
		"message_id", assistantMsg.ID,
	)
	executeResp.MessageID = assistantMsg.ID

	latency := int(time.Since(startTime).Milliseconds())
	observability.L().InfoCtx(ctx, "Chat request completed", "latency_ms", latency)

	return &ChatResponse{
		ConversationID: convID,
		MessageID:      executeResp.MessageID,
		Content:        executeResp.Content,
		Mode:           string(executeResp.Mode),
		InputTokens:    executeResp.InputTokens,
		OutputTokens:   executeResp.OutputTokens,
		LatencyMs:      latency,
		Citations:      s.citationsToInterface(executeResp.Citations),
	}, nil
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
			Role:      msg.Role,
			Content:   msg.Content,
			RouteMode: router.RouteMode(msg.RouteMode),
			CreatedAt: time.Unix(msg.CreatedAt, 0),
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

func deriveLastRouteMode(messages []router.MessageContext) router.RouteMode {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].RouteMode != "" {
			return messages[i].RouteMode
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

// buildExactCacheKey 构建精确缓存的复合键
// 键 = message + mode + stockCode + docType + timeRange
func buildExactCacheKey(message, mode, stockCode, docType, timeRange string) string {
	// 使用简单的拼接方式构建复合键
	// 因为后续会用 MD5 哈希，所以不需要手动处理分隔符
	key := message + "|" + mode + "|" + stockCode + "|" + docType + "|" + timeRange
	return key
}

// truncateString 截断字符串用于日志
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
