package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"stock_rag/internal/metrics"
	"stock_rag/internal/service"
)

// AgentHandler 处理 Agent 相关的请求
type AgentHandler struct {
	taskAgentService *service.TaskAgentService
	jwtSecret        string
}

// NewAgentHandler 创建 Agent 处理器
func NewAgentHandler(taskAgentService *service.TaskAgentService, jwtSecret string) *AgentHandler {
	return &AgentHandler{
		taskAgentService: taskAgentService,
		jwtSecret:        jwtSecret,
	}
}

// ExecuteTask 执行 Agent 任务（使用 Supervisor + Specialists 架构）
func (h *AgentHandler) ExecuteTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	userID := UserIDFromRequest(ctx)

	// 解析请求
	var req struct {
		Task           string                 `json:"task"`
		Params         map[string]interface{} `json:"params,omitempty"`
		ConversationID string                 `json:"conversation_id,omitempty"`
		SessionID      string                 `json:"session_id,omitempty"` // 已废弃，请使用 conversation_id
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Task == "" {
		http.Error(w, "缺少任务描述", http.StatusBadRequest)
		return
	}

	// 生成对话 ID（优先使用 ConversationID，兼容 SessionID）
	conversationID := req.ConversationID
	if conversationID == "" {
		conversationID = req.SessionID
	}
	if conversationID == "" {
		conversationID = fmt.Sprintf("conversation-%d", time.Now().UnixNano())
	}

	// 获取股票代码（从 params 中提取）
	stockCode := ""
	if req.Params != nil {
		if sc, ok := req.Params["stock_code"].(string); ok {
			stockCode = sc
		}
	}

	// 执行 Agent 任务（使用新的 Supervisor + Specialists 架构）
	result, err := h.taskAgentService.ExecuteComplexTask(ctx, &service.ComplexTaskRequest{
		ConversationID: conversationID,
		MessageID:      fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		UserID:         userID,
		UserMessage:    req.Task,
		StockCode:      stockCode,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 返回响应
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := struct {
		Result         string `json:"result"`
		ConversationID string `json:"conversation_id"`
	}{
		Result:         result.Content,
		ConversationID: conversationID,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		return
	}
}

// AnalyzeStock 分析股票（使用 Supervisor + Specialists 架构）
func (h *AgentHandler) AnalyzeStock(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		http.Error(w, "缺少股票代码", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	userID := UserIDFromRequest(ctx)

	// 获取对话 ID（优先使用 conversation_id，兼容 session_id）
	conversationID := r.URL.Query().Get("conversation_id")
	if conversationID == "" {
		conversationID = r.URL.Query().Get("session_id") // 兼容旧接口
	}
	if conversationID == "" {
		conversationID = fmt.Sprintf("conversation-%d", time.Now().UnixNano())
	}

	analyzeStart := time.Now()
	endpoint := "analyze_stock"

	// 执行股票分析（使用新的 Supervisor + Specialists 架构）
	result, err := h.taskAgentService.ExecuteComplexTask(ctx, &service.ComplexTaskRequest{
		ConversationID: conversationID,
		MessageID:      fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		UserID:         userID,
		UserMessage:    fmt.Sprintf("分析股票 %s", symbol),
		StockCode:      symbol,
	})
	elapsed := time.Since(analyzeStart).Seconds()
	if err != nil {
		metrics.RecordAgentComplexTask(endpoint, "error", elapsed)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	status := "success"
	if result == nil || result.Error != "" || result.Content == "" {
		status = "error"
	}
	metrics.RecordAgentComplexTask(endpoint, status, elapsed)
	if result != nil && result.Error != "" {
		http.Error(w, result.Error, http.StatusInternalServerError)
		return
	}

	// 返回响应
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := struct {
		Symbol         string `json:"symbol"`
		ConversationID string `json:"conversation_id"`
		Result         string `json:"result"`
	}{
		Symbol:         symbol,
		ConversationID: conversationID,
		Result:         result.Content,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		return
	}
}

// GetSession 获取会话信息（已废弃）
// Deprecated: 请使用 conversation 相关接口，此端点仅用于向后兼容
// 新架构中 Agent 是无状态的，会话信息存储在外部 ConversationStore 中
func (h *AgentHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	// 获取对话 ID（优先使用 conversation_id，兼容 session_id）
	conversationID := r.URL.Query().Get("conversation_id")
	if conversationID == "" {
		conversationID = r.URL.Query().Get("session_id") // 兼容旧接口
	}
	if conversationID == "" {
		http.Error(w, "缺少 conversation_id 或 session_id", http.StatusBadRequest)
		return
	}

	// 返回兼容响应，提示迁移到新架构
	sessionInfo := struct {
		ConversationID string `json:"conversation_id"`
		Status         string `json:"status"`
		Message        string `json:"message"`
		Deprecated     bool   `json:"deprecated"`
		MigrationHint  string `json:"migration_hint"`
	}{
		ConversationID: conversationID,
		Status:         "stateless_agent",
		Message:        "此接口已废弃，Agent 已改为无状态架构",
		Deprecated:     true,
		MigrationHint:  "请使用 conversation store 获取会话上下文，后续版本将移除此端点",
	}

	// 返回响应
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(sessionInfo); err != nil {
		return
	}
}

// AgentRequest Agent 请求结构
type AgentRequest struct {
	Task           string                 `json:"task"`
	Params         map[string]interface{} `json:"params,omitempty"`
	ConversationID string                 `json:"conversation_id,omitempty"`
	SessionID      string                 `json:"session_id,omitempty"` // 已废弃，请使用 ConversationID
}

// AgentResponse Agent 响应结构
type AgentResponse struct {
	Result         string `json:"result"`
	ConversationID string `json:"conversation_id,omitempty"`
	SessionID      string `json:"session_id,omitempty"` // 已废弃，请使用 ConversationID
}

// RunAgent 运行 Agent（使用 Supervisor + Specialists 架构）
func (h *AgentHandler) RunAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	userID := UserIDFromRequest(ctx)

	// 解析请求
	var req AgentRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Task == "" {
		http.Error(w, "缺少任务描述", http.StatusBadRequest)
		return
	}

	// 生成对话 ID（优先使用 ConversationID，兼容 SessionID）
	conversationID := req.ConversationID
	if conversationID == "" {
		conversationID = req.SessionID
	}
	if conversationID == "" {
		conversationID = fmt.Sprintf("conversation-%d", time.Now().UnixNano())
	}

	// 获取股票代码（从 params 中提取）
	stockCode := ""
	if req.Params != nil {
		if sc, ok := req.Params["stock_code"].(string); ok {
			stockCode = sc
		}
	}

	// 执行 Agent 任务（使用新的 Supervisor + Specialists 架构）
	result, err := h.taskAgentService.ExecuteComplexTask(ctx, &service.ComplexTaskRequest{
		ConversationID: conversationID,
		MessageID:      fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		UserID:         userID,
		UserMessage:    req.Task,
		StockCode:      stockCode,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 返回响应
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := AgentResponse{
		Result:         result.Content,
		ConversationID: conversationID,
		SessionID:      conversationID, // 兼容旧客户端
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		return
	}
}
