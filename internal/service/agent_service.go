package service

import (
	"context"

	"stock_rag/internal/repository"
)

// ComplexTaskRequest 定义复杂任务请求
type ComplexTaskRequest struct {
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	UserID         string `json:"user_id"`
	UserMessage    string `json:"user_message"`
	StockCode      string `json:"stock_code"`
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
	ConversationID string
	MessageID      string
	UserID         string
	UserMessage    string
	StockCode      string
}

// SupervisorAgent 定义 Supervisor Agent 接口
type SupervisorAgent interface {
	ExecuteComplexTask(ctx context.Context, req *ComplexTaskExecuteRequest) (*ComplexTaskResponse, error)
}

// TaskAgentService 负责执行复杂任务（基于 Supervisor + Specialists 架构）
type TaskAgentService struct {
	conversationStore repository.UnifiedConversationStore
	supervisorAgent   SupervisorAgent
}

// NewTaskAgentService 创建 TaskAgentService（基于 Supervisor + Specialists 架构）
func NewTaskAgentService(
	supervisor SupervisorAgent,
	store repository.UnifiedConversationStore,
) *TaskAgentService {
	return &TaskAgentService{
		supervisorAgent:   supervisor,
		conversationStore: store,
	}
}

// ExecuteComplexTask 执行复杂任务（使用 Supervisor + Specialists 架构）
func (s *TaskAgentService) ExecuteComplexTask(ctx context.Context, req *ComplexTaskRequest) (*ComplexTaskResponse, error) {
	// 使用 Supervisor Agent 执行复杂任务
	if s.supervisorAgent != nil {
		executeReq := &ComplexTaskExecuteRequest{
			ConversationID: req.ConversationID,
			MessageID:      req.MessageID,
			UserID:         req.UserID,
			UserMessage:    req.UserMessage,
			StockCode:      req.StockCode,
		}

		resp, err := s.supervisorAgent.ExecuteComplexTask(ctx, executeReq)
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
		}, nil
	}

	return &ComplexTaskResponse{
		MessageID: req.MessageID,
		Error:     "Supervisor Agent not configured",
	}, nil
}
