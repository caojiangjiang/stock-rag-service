package service

import (
	"context"

	"stock_rag/internal/repository"
)

// ComplexTaskRequest 定义复杂任务请求
type ComplexTaskRequest struct {
	ConversationID  string             `json:"conversation_id"`
	MessageID       string             `json:"message_id"`
	UserID          string             `json:"user_id"`
	UserMessage     string             `json:"user_message"`
	StockCode       string             `json:"stock_code"`
	CoordinatorType string             `json:"coordinator_type,omitempty"`
	MemoryContext   string             `json:"memory_context,omitempty"`
	SessionSummary  string             `json:"session_summary,omitempty"`
	OnChunk         func(string) error // SSE 流式回调
}

// ComplexTaskResponse 定义复杂任务响应
type ComplexTaskResponse struct {
	MessageID      string `json:"message_id"`
	Content        string `json:"content"`
	InputTokens    int    `json:"input_tokens"`
	OutputTokens   int    `json:"output_tokens"`
	LatencyMs      int    `json:"latency_ms"`
	Error          string `json:"error"`
	AwaitingHuman  bool   `json:"awaiting_human,omitempty"`
	CheckPointID   string `json:"checkpoint_id,omitempty"`
	InterruptID    string `json:"interrupt_id,omitempty"`
	InterruptInfo  string `json:"interrupt_info,omitempty"`
	PartialContent string `json:"partial_content,omitempty"`
}

// ResumeComplexTaskRequest 人机协同恢复请求
type ResumeComplexTaskRequest struct {
	ConversationID string      `json:"conversation_id"`
	CheckPointID   string      `json:"checkpoint_id"`
	InterruptID    string      `json:"interrupt_id"`
	ResumeData     interface{} `json:"resume_data"`
	UserID         string      `json:"user_id,omitempty"`
	OnChunk        func(string) error
}

// ComplexTaskExecuteRequest 执行请求
type ComplexTaskExecuteRequest struct {
	ConversationID  string
	MessageID       string
	UserID          string
	UserMessage     string
	StockCode       string
	CoordinatorType string
	MemoryContext   string
	SessionSummary  string
	OnChunk         func(string) error // SSE 流式回调
}

// ComplexTaskExecutor 定义复杂任务执行器接口
type ComplexTaskExecutor interface {
	ExecuteComplexTask(ctx context.Context, req *ComplexTaskExecuteRequest) (*ComplexTaskResponse, error)
	ResumeComplexTask(ctx context.Context, req *ResumeComplexTaskExecuteRequest) (*ComplexTaskResponse, error)
}

// ResumeComplexTaskExecuteRequest 恢复执行内部请求
type ResumeComplexTaskExecuteRequest struct {
	ConversationID  string
	CheckPointID    string
	InterruptID     string
	ResumeData      interface{}
	UserID          string
	CoordinatorType string
	OnChunk         func(string) error
}

// TaskAgentService 负责执行复杂任务（委托 ComplexTaskExecutor 实现）。
type TaskAgentService struct {
	conversationStore repository.UnifiedConversationStore
	taskExecutor      ComplexTaskExecutor
}

// NewTaskAgentService 创建 TaskAgentService。
func NewTaskAgentService(
	executor ComplexTaskExecutor,
	store repository.UnifiedConversationStore,
) *TaskAgentService {
	return &TaskAgentService{
		taskExecutor:      executor,
		conversationStore: store,
	}
}

// ExecuteComplexTask 执行复杂任务。
func (s *TaskAgentService) ExecuteComplexTask(ctx context.Context, req *ComplexTaskRequest) (*ComplexTaskResponse, error) {
	if s.taskExecutor == nil {
		return &ComplexTaskResponse{
			MessageID: req.MessageID,
			Error:     "task executor not configured",
		}, nil
	}

	executeReq := &ComplexTaskExecuteRequest{
		ConversationID:  req.ConversationID,
		MessageID:       req.MessageID,
		UserID:          req.UserID,
		UserMessage:     req.UserMessage,
		StockCode:       req.StockCode,
		CoordinatorType: req.CoordinatorType,
		MemoryContext:   req.MemoryContext,
		SessionSummary:  req.SessionSummary,
		OnChunk:         req.OnChunk, // 透传流式回调
	}

	resp, err := s.taskExecutor.ExecuteComplexTask(ctx, executeReq)
	if err != nil {
		return &ComplexTaskResponse{
			MessageID: req.MessageID,
			Error:     err.Error(),
		}, nil
	}

	s.syncConversationStatus(ctx, req.ConversationID, resp)

	return &ComplexTaskResponse{
		MessageID:      resp.MessageID,
		Content:        resp.Content,
		InputTokens:    resp.InputTokens,
		OutputTokens:   resp.OutputTokens,
		LatencyMs:      resp.LatencyMs,
		Error:          resp.Error,
		AwaitingHuman:  resp.AwaitingHuman,
		CheckPointID:   resp.CheckPointID,
		InterruptID:    resp.InterruptID,
		InterruptInfo:  resp.InterruptInfo,
		PartialContent: resp.PartialContent,
	}, nil
}

// ResumeComplexTask 从 HITL 中断点恢复执行。
func (s *TaskAgentService) ResumeComplexTask(ctx context.Context, req *ResumeComplexTaskRequest) (*ComplexTaskResponse, error) {
	if s.taskExecutor == nil {
		return &ComplexTaskResponse{Error: "task executor not configured"}, nil
	}
	executeReq := &ResumeComplexTaskExecuteRequest{
		ConversationID: req.ConversationID,
		CheckPointID:   req.CheckPointID,
		InterruptID:    req.InterruptID,
		ResumeData:     req.ResumeData,
		UserID:         req.UserID,
		OnChunk:        req.OnChunk,
	}
	resp, err := s.taskExecutor.ResumeComplexTask(ctx, executeReq)
	if err != nil {
		return &ComplexTaskResponse{Error: err.Error()}, nil
	}
	s.syncConversationStatus(ctx, req.ConversationID, resp)
	return resp, nil
}

func (s *TaskAgentService) syncConversationStatus(ctx context.Context, conversationID string, resp *ComplexTaskResponse) {
	if s.conversationStore == nil || conversationID == "" || resp == nil {
		return
	}
	switch {
	case resp.AwaitingHuman:
		_ = repository.SetConversationStatus(ctx, s.conversationStore, conversationID, repository.ConversationStatusPendingHuman)
	case resp.Error != "":
		_ = repository.SetConversationStatus(ctx, s.conversationStore, conversationID, repository.ConversationStatusActive)
	default:
		_ = repository.SetConversationStatus(ctx, s.conversationStore, conversationID, repository.ConversationStatusActive)
	}
}
