package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"stock_rag/internal/observability"
)

// PeerCoordinator 并行协作协调器
// 多个 Agent 并行处理同一任务，然后汇总结果
type PeerCoordinator struct {
	*BaseCoordinator
}

// NewPeerCoordinator 创建并行协调器
func NewPeerCoordinator(profileRegistry *ProfileRegistry, agentBuilder *AgentBuilder) *PeerCoordinator {
	base := NewBaseCoordinator("peer", profileRegistry, agentBuilder)
	return &PeerCoordinator{
		BaseCoordinator: base,
	}
}

// Execute 执行并行协调逻辑
func (c *PeerCoordinator) Execute(ctx context.Context, taskState *TaskState) (string, error) {
	profiles := c.GetAgentProfiles()
	if len(profiles) == 0 {
		return "没有配置任何 Agent", nil
	}

	taskState.UpdateStatus(TaskStatusRunning)

	var wg sync.WaitGroup
	results := make([]string, len(profiles))
	errs := make([]error, len(profiles))
	mu := sync.Mutex{}

	// 并行执行所有 Agent
	for i, profile := range profiles {
		wg.Add(1)
		go func(idx int, prof *AgentProfile) {
			defer wg.Done()

			stepStart := time.Now()
			stepTrace := StepTrace{
				StepID:    fmt.Sprintf("peer_%d", idx+1),
				ToolName:  prof.Name,
				Input:     map[string]interface{}{"query": taskState.UserMessage},
				StartTime: stepStart,
				Status:    TaskStatusRunning,
			}

			output, err := c.executeStep(ctx, prof, taskState)

			stepTrace.EndTime = time.Now()
			if err != nil {
				stepTrace.Status = TaskStatusFailed
				stepTrace.Error = err.Error()
			} else {
				stepTrace.Status = TaskStatusCompleted
				stepTrace.Output = output
			}

			mu.Lock()
			taskState.AddStepTrace(stepTrace)
			results[idx] = output
			errs[idx] = err
			mu.Unlock()
		}(i, profile)
	}

	wg.Wait()

	// 检查错误
	for i, err := range errs {
		if err != nil {
			taskState.UpdateStatus(TaskStatusFailed)
			taskState.AddError(fmt.Sprintf("Agent %s 执行失败: %v", profiles[i].Name, err))
		}
	}

	// 汇总结果
	summary := c.summarizeResults(results, profiles)
	taskState.Summary = summary
	taskState.UpdateStatus(TaskStatusCompleted)

	return summary, nil
}

// executeStep 执行单个步骤
// 真正调用对应 profile 的工具来执行任务
func (c *PeerCoordinator) executeStep(ctx context.Context, profile *AgentProfile, taskState *TaskState) (string, error) {
	// 使用 profile 中配置的工具执行任务
	if len(profile.AvailableTools) > 0 {
		return c.executeWithTools(ctx, profile, taskState)
	}

	// 回退：返回默认信息
	return fmt.Sprintf("[%s] 处理完成", profile.Role), nil
}

// executeWithTools 使用 profile 中配置的工具执行任务
func (c *PeerCoordinator) executeWithTools(ctx context.Context, profile *AgentProfile, taskState *TaskState) (string, error) {
	var results []string

	// 依次执行 profile 中配置的工具
	for _, toolName := range profile.AvailableTools {
		result, err := c.executeWithTool(ctx, toolName, taskState)
		if err != nil {
			results = append(results, fmt.Sprintf("工具 %s 执行失败: %v", toolName, err))
		} else {
			results = append(results, fmt.Sprintf("工具 %s 执行结果:\n%s", toolName, result))
		}
	}

	if len(results) > 0 {
		return fmt.Sprintf("[%s]\n%s", profile.Role, joinStrings(results, "\n\n")), nil
	}

	return fmt.Sprintf("[%s] 处理完成", profile.Role), nil
}

// executeWithTool 执行单个工具
func (c *PeerCoordinator) executeWithTool(ctx context.Context, toolName string, taskState *TaskState) (string, error) {
	result, err := c.InvokeTool(ctx, toolName, ToolParamsFromTask(taskState))
	if err != nil {
		return "", fmt.Errorf("工具 %s 执行失败: %v", toolName, err)
	}
	return result, nil
}

// summarizeResults 汇总并行结果
func (c *PeerCoordinator) summarizeResults(results []string, profiles []*AgentProfile) string {
	var summary string
	summary += "【并行协作结果汇总】\n\n"
	for i, result := range results {
		if result != "" {
			summary += fmt.Sprintf("%s:\n%s\n\n", profiles[i].Role, result)
		}
	}
	summary += "【综合结论】综合各专家意见，已完成分析。"
	return summary
}

// joinStrings 连接字符串切片
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}

// ============ 通用 Fanout 并发执行框架 ============

// FanoutResult 单个任务的执行结果
type FanoutResult[T any] struct {
	Index    int
	Result   T
	Error    error
	TaskName string
}

// FanoutOptions Fanout 执行选项
type FanoutOptions struct {
	Timeout        time.Duration
	MaxConcurrency int
	EnableTracing  bool
	TraceName      string
}

// FanoutOption 选项函数
type FanoutOption func(*FanoutOptions)

// WithTimeout 设置超时时间
func WithTimeout(timeout time.Duration) FanoutOption {
	return func(o *FanoutOptions) {
		o.Timeout = timeout
	}
}

// WithMaxConcurrency 设置最大并发数
func WithMaxConcurrency(max int) FanoutOption {
	return func(o *FanoutOptions) {
		o.MaxConcurrency = max
	}
}

// WithTracing 启用追踪
func WithTracing(name string) FanoutOption {
	return func(o *FanoutOptions) {
		o.EnableTracing = true
		o.TraceName = name
	}
}

// Fanout 通用并发执行函数
// items: 待处理的项目列表
// task: 每个项目的处理函数
// options: 执行选项
func Fanout[T any, I any](ctx context.Context, items []I, task func(context.Context, int, I) (T, error), options ...FanoutOption) []FanoutResult[T] {
	// 设置默认选项
	opts := &FanoutOptions{
		Timeout:        60 * time.Second,
		MaxConcurrency: len(items),
		EnableTracing:  true,
		TraceName:      "peer.fanout",
	}

	for _, opt := range options {
		opt(opts)
	}

	// 限制并发数
	if opts.MaxConcurrency <= 0 || opts.MaxConcurrency > len(items) {
		opts.MaxConcurrency = len(items)
	}

	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	// 创建结果通道
	resultCh := make(chan FanoutResult[T], len(items))

	// 限制并发的 semaphore
	sem := make(chan struct{}, opts.MaxConcurrency)

	// 并发执行
	for i, item := range items {
		go func(idx int, it I) {
			// 获取 semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			var span trace.Span
			var spanCtx = ctx
			if opts.EnableTracing {
				spanCtx, span = observability.StartSpan(ctx, fmt.Sprintf("%s.item", opts.TraceName))
				defer span.End()
			}

			result, err := task(spanCtx, idx, it)

			if span != nil {
				if err != nil {
					span.SetAttributes(attribute.String("error", err.Error()))
				}
				span.SetAttributes(attribute.String("task_name", fmt.Sprintf("item_%d", idx)))
			}

			resultCh <- FanoutResult[T]{
				Index:    idx,
				Result:   result,
				Error:    err,
				TaskName: fmt.Sprintf("item_%d", idx),
			}
		}(i, item)
	}

	// 收集结果
	results := make([]FanoutResult[T], len(items))
	for i := 0; i < len(items); i++ {
		select {
		case result := <-resultCh:
			results[result.Index] = result
		case <-ctx.Done():
			// 超时情况下，返回已完成的结果
			return results[:i]
		}
	}

	return results
}
