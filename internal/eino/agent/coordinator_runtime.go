package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/adk"
	"go.opentelemetry.io/otel/attribute"

	"stock_rag/internal/metrics"
	"stock_rag/internal/observability"
	"stock_rag/internal/pkgctx"
)

type coordinatorContextKey struct{}

// CoordinatorRuntime 多 Agent 协调运行时（超时、重试、指标）。
type CoordinatorRuntime struct {
	CoordinatorName string
	Strategy        pkgctx.ExecutorStrategyConfig
	retry           *RetryHandler
}

// NewCoordinatorRuntime 创建协调器运行时。
func NewCoordinatorRuntime(name string, strategy *pkgctx.ExecutorStrategyConfig) *CoordinatorRuntime {
	cfg := pkgctx.DefaultExecutorStrategy()
	if strategy != nil {
		cfg = *strategy
	}
	cfg.Validate()
	return &CoordinatorRuntime{
		CoordinatorName: name,
		Strategy:        cfg,
		retry:           NewRetryHandler(&cfg),
	}
}

// WithRuntime 将运行时写入 context。
func WithRuntime(ctx context.Context, rt *CoordinatorRuntime) context.Context {
	if rt == nil {
		return ctx
	}
	return context.WithValue(ctx, coordinatorContextKey{}, rt)
}

// RuntimeFromContext 从 context 读取运行时，不存在则返回 nil。
func RuntimeFromContext(ctx context.Context) *CoordinatorRuntime {
	if ctx == nil {
		return nil
	}
	rt, _ := ctx.Value(coordinatorContextKey{}).(*CoordinatorRuntime)
	return rt
}

// DeriveContext 为协调器执行派生带超时的 context。
func (rt *CoordinatorRuntime) DeriveContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	timeout := time.Duration(rt.Strategy.SubAgentTimeoutMs) * time.Millisecond
	if deadline, ok := parent.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	return context.WithTimeout(parent, timeout)
}

// RunSubTask 执行子任务，失败时按策略局部重试。
func (rt *CoordinatorRuntime) RunSubTask(
	ctx context.Context,
	taskState *TaskState,
	subtaskName string,
	fn func(context.Context) (string, error),
) (string, error) {
	handler := NewRetryHandler(&rt.Strategy)
	var lastErr error
	var output string
	stepIndex := 0

	for handler.ShouldRetry() {
		attempt := handler.currentRetry + 1
		start := time.Now()

		// 为每个步骤创建 span
		stepCtx, spanEnd := StartCoordinatorStepSpan(ctx, rt.CoordinatorName, subtaskName, stepIndex)

		deriveCtx, cancel := rt.DeriveContext(stepCtx)
		result, err := fn(deriveCtx)
		cancel()

		elapsed := time.Since(start).Seconds()
		status := "success"

		if err != nil {
			status = "error"
			lastErr = err
			rt.recordSubtask(taskState, subtaskName, TaskStatusFailed, "", err.Error(), start)
			handler.RecordRetry()

			// 记录步骤级 metrics
			metrics.RecordAgentStep(rt.CoordinatorName, subtaskName, status, elapsed)
			spanEnd()

			if !handler.ShouldRetry() {
				break
			}
			delay := time.Duration(handler.GetDelayMs()) * time.Millisecond
			observability.L().WarnCtx(ctx, "Subtask failed, retrying",
				"coordinator", rt.CoordinatorName,
				"subtask", subtaskName,
				"attempt", attempt,
				"delay_ms", delay.Milliseconds(),
				"error", err.Error(),
			)
			time.Sleep(delay)
			stepIndex++
			continue
		}

		output = result
		rt.recordSubtask(taskState, subtaskName, TaskStatusCompleted, result, "", start)

		// 记录步骤级 metrics
		metrics.RecordAgentStep(rt.CoordinatorName, subtaskName, status, elapsed)
		spanEnd()

		metrics.RecordAgentSubtask(rt.CoordinatorName, subtaskName, status, elapsed)
		return output, nil
	}

	metrics.RecordAgentSubtask(rt.CoordinatorName, subtaskName, "error", 0)
	return output, lastErr
}

func (rt *CoordinatorRuntime) recordSubtask(
	taskState *TaskState,
	subtaskName string,
	status TaskStatus,
	output, errMsg string,
	start time.Time,
) {
	if taskState == nil {
		return
	}
	trace := StepTrace{
		StepID:    fmt.Sprintf("%d", taskState.CurrentStep+1),
		ToolName:  subtaskName,
		Output:    output,
		Error:     errMsg,
		StartTime: start,
		EndTime:   time.Now(),
		Status:    status,
	}
	trace.LatencyMS = trace.EndTime.Sub(trace.StartTime).Milliseconds()
	taskState.AddStepTrace(trace)
	// 记录步骤级指标
	statusStr := "success"
	if status == TaskStatusFailed {
		statusStr = "error"
	}
	metrics.RecordAgentStep(rt.CoordinatorName, subtaskName, statusStr, time.Since(start).Seconds())
}

// ProcessADKIterator 统一处理 ADK 事件流，记录子 Agent 步骤与指标。
func (rt *CoordinatorRuntime) ProcessADKIterator(
	ctx context.Context,
	taskState *TaskState,
	iterator *adk.AsyncIterator[*adk.AgentEvent],
) (string, error) {
	var finalResult string

	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}

		if event == nil {
			continue
		}

		if event.Err != nil {
			agentName := eventAgentName(event)
			rt.recordSubtask(taskState, agentName, TaskStatusFailed, "", event.Err.Error(), time.Now())
			metrics.RecordAgentSubtask(rt.CoordinatorName, agentName, "error", 0)
			taskState.AddError(fmt.Sprintf("[%s] %v", agentName, event.Err))
			return finalResult, event.Err
		}

		if event.Output != nil && event.Output.MessageOutput != nil {
			msg, _ := event.Output.MessageOutput.GetMessage()
			if msg == nil {
				continue
			}
			content := msg.Content
			finalResult += content

			// 如果有流式回调，立即推送内容
			if taskState.OnChunk != nil && content != "" {
				if err := taskState.OnChunk(content); err != nil {
					return finalResult, err
				}
			}

			agentName := eventAgentName(event)
			start := time.Now()
			rt.recordSubtask(taskState, agentName, TaskStatusCompleted, content, "", start)
			metrics.RecordAgentSubtask(rt.CoordinatorName, agentName, "success", time.Since(start).Seconds())
		}

		if event.Action != nil && event.Action.TransferToAgent != nil {
			dest := event.Action.TransferToAgent.DestAgentName
			taskState.AddFinding(fmt.Sprintf("转移到 Agent: %s", dest))
			observability.L().InfoCtx(ctx, "Agent transfer",
				"coordinator", rt.CoordinatorName,
				"dest_agent", dest,
			)
		}
	}

	return finalResult, nil
}

func eventAgentName(event *adk.AgentEvent) string {
	if event.AgentName != "" {
		return event.AgentName
	}
	return "unknown_agent"
}

// EnrichTaskState 注入路由上下文到任务状态。
func EnrichTaskState(taskState *TaskState, stockCode string) {
	if taskState == nil {
		return
	}
	if stockCode != "" {
		taskState.StockCode = stockCode
	}
}

// RecordCoordinatorResult 记录协调器整体执行结果指标。
func RecordCoordinatorResult(coordinator, classifier, status string, seconds float64) {
	metrics.RecordAgentCoordinator(coordinator, classifier, status, seconds)
}

// StartCoordinatorStepSpan 为协调器内的单个步骤创建 span。
func StartCoordinatorStepSpan(ctx context.Context, coordinator, stepName string, stepIndex int) (context.Context, func()) {
	ctx, span := observability.StartSpan(ctx, fmt.Sprintf("%s.step.%s", coordinator, stepName))
	span.SetAttributes(
		attribute.String("coordinator.name", coordinator),
		attribute.String("step.name", stepName),
		attribute.Int("step.index", stepIndex),
	)
	return ctx, func() {
		span.End()
	}
}

// StartCoordinatorSpan 为协调器执行添加 span 属性。
func StartCoordinatorSpan(ctx context.Context, coordinator string, taskState *TaskState) (context.Context, func()) {
	ctx, span := observability.StartSpan(ctx, coordinator+".Execute")
	if taskState != nil {
		span.SetAttributes(
			attribute.String("coordinator.name", coordinator),
			attribute.String("task.conversation_id", taskState.ConversationID),
			attribute.String("task.message_id", taskState.MessageID),
			attribute.String("task.stock_code", taskState.StockCode),
		)
	}
	return ctx, func() {
		if taskState != nil {
			span.SetAttributes(
				attribute.Int("task.step_count", taskState.CurrentStep),
				attribute.String("task.status", string(taskState.Status)),
				attribute.Int("task.retry_count", taskState.RetryCount),
			)
		}
		span.End()
	}
}
