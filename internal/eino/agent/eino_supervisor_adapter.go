package agent

import (
	"context"
	"time"

	"stock_rag/internal/metrics"
	"stock_rag/internal/observability"
	"stock_rag/internal/pkgctx"
	"stock_rag/internal/service"
)

// CoordinatorSupervisorAdapter 使用 Coordinator 体系的 Supervisor 适配器
// 这是推荐的新实现方式，适配 service.SupervisorAgent 接口
type CoordinatorSupervisorAdapter struct {
	coordinator Coordinator
	runtime     *CoordinatorRuntime
}

// NewCoordinatorSupervisorAdapter 创建基于 Coordinator 的 Supervisor 适配器
func NewCoordinatorSupervisorAdapter(coordinator Coordinator) *CoordinatorSupervisorAdapter {
	name := "unknown"
	if coordinator != nil {
		name = coordinator.Name()
	}
	return &CoordinatorSupervisorAdapter{
		coordinator: coordinator,
		runtime:     NewCoordinatorRuntime(name, nil),
	}
}

// NewCoordinatorSupervisorAdapterWithStrategy 创建带执行策略的适配器。
func NewCoordinatorSupervisorAdapterWithStrategy(coordinator Coordinator, strategy *pkgctx.ExecutorStrategyConfig) *CoordinatorSupervisorAdapter {
	name := "unknown"
	if coordinator != nil {
		name = coordinator.Name()
	}
	return &CoordinatorSupervisorAdapter{
		coordinator: coordinator,
		runtime:     NewCoordinatorRuntime(name, strategy),
	}
}

// ExecuteComplexTask 执行复杂任务，使用 Coordinator 体系（含超时与协调器重试）。
func (a *CoordinatorSupervisorAdapter) ExecuteComplexTask(ctx context.Context, req *service.ComplexTaskExecuteRequest) (*service.ComplexTaskResponse, error) {
	start := time.Now()
	endpoint := "complex_task"
	defer func() {
		// 由调用方按 endpoint 覆盖时可再封装；此处记录通用复杂任务
		_ = endpoint
	}()

	if a.coordinator == nil {
		metrics.RecordAgentComplexTask(endpoint, "error", time.Since(start).Seconds())
		return &service.ComplexTaskResponse{
			MessageID: req.MessageID,
			Error:     "coordinator not configured",
		}, nil
	}

	coordinatorName := a.coordinator.Name()
	handler := NewRetryHandler(&a.runtime.Strategy)
	var lastErr error

	for handler.ShouldRetry() {
		attempt := handler.currentRetry + 1
		taskState := NewTaskState(req.ConversationID, req.MessageID, req.UserID, req.UserMessage)
		EnrichTaskState(taskState, req.StockCode)
		taskState.RetryCount = attempt - 1

		runCtx, cancel := a.runtime.DeriveContext(ctx)
		runCtx = WithRuntime(runCtx, a.runtime)

		result, err := a.coordinator.Execute(runCtx, taskState)
		cancel()

		latencyMs := int(time.Since(start).Milliseconds())

		if err == nil {
			status := "success"
			if taskState.Status == TaskStatusFailed {
				status = "error"
			}
			RecordCoordinatorResult(coordinatorName, status, time.Since(start).Seconds())
			metrics.RecordAgentComplexTask(endpoint, status, time.Since(start).Seconds())
			return &service.ComplexTaskResponse{
				MessageID: req.MessageID,
				Content:   result,
				LatencyMs: latencyMs,
			}, nil
		}

		lastErr = err
		taskState.AddError(err.Error())
		handler.RecordRetry()
		if !handler.ShouldRetry() {
			break
		}
		delay := time.Duration(handler.GetDelayMs()) * time.Millisecond
		observability.L().WarnCtx(ctx, "Coordinator execution failed, retrying",
			"coordinator", coordinatorName,
			"attempt", attempt,
			"delay_ms", delay.Milliseconds(),
			"error", err.Error(),
		)
		time.Sleep(delay)
	}

	RecordCoordinatorResult(coordinatorName, "error", time.Since(start).Seconds())
	metrics.RecordAgentComplexTask(endpoint, "error", time.Since(start).Seconds())
	return &service.ComplexTaskResponse{
		MessageID: req.MessageID,
		Error:     lastErr.Error(),
		LatencyMs: int(time.Since(start).Milliseconds()),
	}, nil
}

// CoordinatorRunner 是 Coordinator 的运行器接口
// 提供统一的任务执行入口
type CoordinatorRunner interface {
	Execute(ctx context.Context, req *service.ComplexTaskExecuteRequest) (*service.ComplexTaskResponse, error)
}

// NewCoordinatorRunner 创建 Coordinator 运行器
func NewCoordinatorRunner(coordinator Coordinator) CoordinatorRunner {
	return &CoordinatorSupervisorAdapter{coordinator: coordinator, runtime: NewCoordinatorRuntime(coordinator.Name(), nil)}
}

// Execute 执行任务（实现 CoordinatorRunner 接口）
func (a *CoordinatorSupervisorAdapter) Execute(ctx context.Context, req *service.ComplexTaskExecuteRequest) (*service.ComplexTaskResponse, error) {
	return a.ExecuteComplexTask(ctx, req)
}
