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
	OnChunk         func(string) error // SSE 流式回调
}

// ComplexTaskResponse 定义复杂任务响应
type ComplexTaskResponse struct {
	MessageID    string `json:"message_id"`
	Content      string `json:"content"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	LatencyMs    int    `json:"latency_ms"`
	Error        string `json:"error"`
}

// ComplexTaskExecuteRequest 执行请求
type ComplexTaskExecuteRequest struct {
	ConversationID  string
	MessageID       string
	UserID          string
	UserMessage     string
	StockCode       string
	CoordinatorType string
	OnChunk         func(string) error // SSE 流式回调
}

// ComplexTaskExecutor 定义复杂任务执行器接口
type ComplexTaskExecutor interface {
	ExecuteComplexTask(ctx context.Context, req *ComplexTaskExecuteRequest) (*ComplexTaskResponse, error)
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
		OnChunk:         req.OnChunk, // 透传流式回调
	}

	resp, err := s.taskExecutor.ExecuteComplexTask(ctx, executeReq)
	if err != nil {
		return &ComplexTaskResponse{
			MessageID: req.MessageID,
			Error:     err.Error(),
		}, nil
	}

	return &ComplexTaskResponse{
		MessageID:    resp.MessageID,
		Content:      resp.Content,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		LatencyMs:    resp.LatencyMs,
		Error:        resp.Error,
	}, nil
}
