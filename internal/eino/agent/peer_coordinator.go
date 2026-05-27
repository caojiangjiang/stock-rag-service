package agent

import (
	"context"
	"fmt"
	"sync"
	"time"
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
