package agent

import (
	"context"

	"stock_rag/internal/service"
)

// CoordinatorSupervisorAdapter 使用 Coordinator 体系的 Supervisor 适配器
// 这是推荐的新实现方式，适配 service.SupervisorAgent 接口
type CoordinatorSupervisorAdapter struct {
	coordinator Coordinator
}

// NewCoordinatorSupervisorAdapter 创建基于 Coordinator 的 Supervisor 适配器
func NewCoordinatorSupervisorAdapter(coordinator Coordinator) *CoordinatorSupervisorAdapter {
	return &CoordinatorSupervisorAdapter{
		coordinator: coordinator,
	}
}

// ExecuteComplexTask 执行复杂任务，使用 Coordinator 体系
func (a *CoordinatorSupervisorAdapter) ExecuteComplexTask(ctx context.Context, req *service.ComplexTaskExecuteRequest) (*service.ComplexTaskResponse, error) {
	taskState := NewTaskState(
		req.ConversationID,
		req.MessageID,
		req.UserID,
		req.UserMessage,
	)

	if req.StockCode != "" {
		taskState.StockCode = req.StockCode
	}

	result, err := a.coordinator.Execute(ctx, taskState)
	if err != nil {
		return &service.ComplexTaskResponse{
			MessageID: req.MessageID,
			Error:     err.Error(),
		}, nil
	}

	return &service.ComplexTaskResponse{
		MessageID:    req.MessageID,
		Content:      result,
		InputTokens:  0,
		OutputTokens: 0,
		LatencyMs:    0,
	}, nil
}

// CoordinatorRunner 是 Coordinator 的运行器接口
// 提供统一的任务执行入口
type CoordinatorRunner interface {
	Execute(ctx context.Context, req *service.ComplexTaskExecuteRequest) (*service.ComplexTaskResponse, error)
}

// NewCoordinatorRunner 创建 Coordinator 运行器
func NewCoordinatorRunner(coordinator Coordinator) CoordinatorRunner {
	return &CoordinatorSupervisorAdapter{coordinator: coordinator}
}

// Execute 执行任务（实现 CoordinatorRunner 接口）
func (a *CoordinatorSupervisorAdapter) Execute(ctx context.Context, req *service.ComplexTaskExecuteRequest) (*service.ComplexTaskResponse, error) {
	return a.ExecuteComplexTask(ctx, req)
}
