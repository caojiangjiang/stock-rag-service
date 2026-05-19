package agent

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"stock_rag/internal/repository"
	"stock_rag/internal/router"
)

const (
	chatRecentMessageLimit = 5
	chatTitleRuneLimit     = 50
)

type ChatService struct {
	routeEngine  *router.RouteEngine
	executor     *AgentExecutor
	conversation repository.UnifiedConversationStore
}

func NewChatService(
	routeEngine *router.RouteEngine,
	executor *AgentExecutor,
	conversation repository.UnifiedConversationStore,
) *ChatService {
	return &ChatService{
		routeEngine:  routeEngine,
		executor:     executor,
		conversation: conversation,
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
	if req == nil {
		err := fmt.Errorf("chat request is nil")
		return &ChatResponse{Error: err.Error()}, err
	}

	startTime := time.Now()
	req.Message = strings.TrimSpace(req.Message)
	convID := strings.TrimSpace(req.ConversationID)
	if convID == "" {
		convID = newConversationID()
		log.Printf("[AgentService] Generated new conversation ID: %s", convID)
	}

	log.Printf("[AgentService] Starting chat request - ConversationID: %s, UserID: %s, MessagePreview: %s",
		convID, req.UserID, truncateMessageForLog(req.Message, 120))

	var explicitMode router.RouteMode
	if req.Mode != "" {
		explicitMode = router.RouteMode(req.Mode)
		log.Printf("[AgentService] Explicit mode specified: %s", explicitMode)
	}

	log.Printf("[AgentService] Fetching recent messages and conversation context")
	recentMessages, err := s.getRecentMessages(ctx, convID, chatRecentMessageLimit)
	if err != nil {
		log.Printf("[AgentService] Failed to load recent messages: %v", err)
		recentMessages = []router.MessageContext{}
	}
	log.Printf("[AgentService] Found %d recent messages", len(recentMessages))

	summary, err := s.getConversationSummary(ctx, convID)
	if err != nil {
		log.Printf("[AgentService] Failed to load conversation summary: %v", err)
		summary = ""
	}

	lastRouteMode := deriveLastRouteMode(recentMessages)
	if lastRouteMode == "" {
		lastRouteMode = s.getLastRouteMode(ctx, convID)
	}
	log.Printf("[AgentService] Last route mode: %s", lastRouteMode)

	userMsg := repository.NewMessage(convID, "user", req.Message, nil)

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

	log.Printf("[AgentService] Starting route decision")
	routeDecision, err := s.routeEngine.Route(ctx, routeInput)
	if err != nil {
		log.Printf("[AgentService] Route decision failed: %v", err)
		return &ChatResponse{
			ConversationID: convID,
			Error:          "路由失败: " + err.Error(),
		}, err
	}
	log.Printf("[AgentService] Route decision completed - SelectedMode: %s, MessageID: %s",
		routeDecision.SelectedMode, routeDecision.MessageID)

	userMsg.RouteMode = string(routeDecision.SelectedMode)

	if err := s.ensureConversationExists(ctx, convID, req.UserID, req.Message); err != nil {
		log.Printf("[AgentService] Failed to ensure conversation exists: %v", err)
		return &ChatResponse{
			ConversationID: convID,
			Error:          "创建会话失败: " + err.Error(),
		}, err
	}

	if err := s.conversation.SaveMessage(ctx, userMsg); err != nil {
		log.Printf("[AgentService] Failed to save user message: %v", err)
		return &ChatResponse{
			ConversationID: convID,
			Error:          "保存用户消息失败: " + err.Error(),
		}, err
	}
	log.Printf("[AgentService] User message saved - ConversationID: %s, MessageID: %s", convID, userMsg.ID)

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

	log.Printf("[AgentService] Executing request - ConversationID: %s, MessageID: %s, Mode: %s",
		convID, userMsg.ID, routeDecision.SelectedMode)
	executeResp, err := s.executor.Execute(ctx, executeReq)
	if err != nil {
		log.Printf("[AgentService] Execution failed: %v", err)
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
		log.Printf("[AgentService] Execution returned error: %s", executeResp.Error)
		return &ChatResponse{
			ConversationID: convID,
			Error:          executeResp.Error,
		}, nil
	}

	log.Printf("[AgentService] Execution completed - MessageID: %s, ContentLength: %d, InputTokens: %d, OutputTokens: %d",
		executeResp.MessageID, len(executeResp.Content), executeResp.InputTokens, executeResp.OutputTokens)

	assistantMsg := repository.NewMessage(convID, "assistant", executeResp.Content, nil)
	if executeResp.MessageID != "" {
		assistantMsg.ID = executeResp.MessageID
	}
	assistantMsg.RouteMode = string(routeDecision.SelectedMode)
	if err := s.conversation.SaveMessage(ctx, assistantMsg); err != nil {
		log.Printf("[AgentService] Failed to save assistant message: %v", err)
		return &ChatResponse{
			ConversationID: convID,
			Error:          "保存助手消息失败: " + err.Error(),
		}, err
	}
	log.Printf("[AgentService] Assistant message saved - ConversationID: %s, MessageID: %s", convID, assistantMsg.ID)
	executeResp.MessageID = assistantMsg.ID

	latency := int(time.Since(startTime).Milliseconds())
	log.Printf("[AgentService] Chat request completed - Total latency: %dms", latency)

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
