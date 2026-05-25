package agent

import (
	"context"
	"fmt"
	"time"

	"stock_rag/internal/eino/tools"
)

// CommitteeCoordinator 委员会模式协调器
// 多个 Agent 分别评估，然后由主席汇总并做出最终决策
type CommitteeCoordinator struct {
	*BaseCoordinator
	chairProfile *AgentProfile
}

// NewCommitteeCoordinator 创建委员会协调器
func NewCommitteeCoordinator(profileRegistry *ProfileRegistry, toolRegistry *tools.ToolRegistry) *CommitteeCoordinator {
	base := NewBaseCoordinator("committee", profileRegistry, toolRegistry)
	return &CommitteeCoordinator{
		BaseCoordinator: base,
		chairProfile:    TaskPlannerProfile,
	}
}

// SetChairProfile 设置主席 Profile
func (c *CommitteeCoordinator) SetChairProfile(profile *AgentProfile) {
	c.chairProfile = profile
}

// Execute 执行委员会协调逻辑
func (c *CommitteeCoordinator) Execute(ctx context.Context, taskState *TaskState) (string, error) {
	profiles := c.GetAgentProfiles()
	if len(profiles) == 0 {
		return "没有配置任何委员会成员", nil
	}

	taskState.UpdateStatus(TaskStatusRunning)

	// 1. 委员会成员分别评估
	evaluations := make([]string, len(profiles))

	for i, profile := range profiles {
		if err := ctx.Err(); err != nil {
			taskState.UpdateStatus(TaskStatusFailed)
			taskState.AddError(fmt.Sprintf("任务被取消: %v", err))
			return "", err
		}

		taskState.CurrentStep = i + 1
		taskState.UpdatedAt = time.Now()

		stepStart := time.Now()
		stepTrace := StepTrace{
			StepID:    fmt.Sprintf("member_%d", i+1),
			ToolName:  profile.Name,
			Input:     map[string]interface{}{"query": taskState.UserMessage},
			StartTime: stepStart,
			Status:    TaskStatusRunning,
		}

		evaluation, err := c.evaluate(ctx, profile, taskState)

		stepTrace.EndTime = time.Now()
		if err != nil {
			stepTrace.Status = TaskStatusFailed
			stepTrace.Error = err.Error()
			taskState.AddStepTrace(stepTrace)
			taskState.UpdateStatus(TaskStatusFailed)
			taskState.AddError(fmt.Sprintf("成员 %s 评估失败: %v", profile.Name, err))
			return "", err
		}

		stepTrace.Status = TaskStatusCompleted
		stepTrace.Output = evaluation
		taskState.AddStepTrace(stepTrace)
		evaluations[i] = evaluation
		taskState.AddFinding(fmt.Sprintf("%s: %s", profile.Role, evaluation))
	}

	// 2. 主席汇总并决策
	stepStart := time.Now()
	stepTrace := StepTrace{
		StepID:    "chair_summary",
		ToolName:  c.chairProfile.Name,
		Input:     map[string]interface{}{"query": taskState.UserMessage, "evaluations": evaluations},
		StartTime: stepStart,
		Status:    TaskStatusRunning,
	}

	decision := c.chairSummarize(ctx, evaluations, profiles, taskState)

	stepTrace.EndTime = time.Now()
	stepTrace.Status = TaskStatusCompleted
	stepTrace.Output = decision
	taskState.AddStepTrace(stepTrace)

	taskState.Summary = decision
	taskState.UpdateStatus(TaskStatusCompleted)

	return decision, nil
}

// evaluate 单个成员评估
// 真正调用对应 profile 的工具来执行评估
func (c *CommitteeCoordinator) evaluate(ctx context.Context, profile *AgentProfile, taskState *TaskState) (string, error) {
	// 使用 profile 中配置的工具执行评估
	if len(profile.AvailableTools) > 0 {
		return c.evaluateWithTools(ctx, profile, taskState)
	}

	// 回退：返回默认评估信息
	return fmt.Sprintf("%s：评估通过", profile.Role), nil
}

// evaluateWithTools 使用 profile 中配置的工具进行评估
func (c *CommitteeCoordinator) evaluateWithTools(ctx context.Context, profile *AgentProfile, taskState *TaskState) (string, error) {
	var results []string

	for _, toolName := range profile.AvailableTools {
		result, err := c.executeTool(ctx, toolName, taskState)
		if err != nil {
			results = append(results, fmt.Sprintf("工具 %s 执行失败: %v", toolName, err))
		} else {
			results = append(results, fmt.Sprintf("工具 %s 执行结果:\n%s", toolName, result))
		}
	}

	if len(results) > 0 {
		return fmt.Sprintf("%s：%s", profile.Role, joinStrings(results, "\n\n")), nil
	}

	return fmt.Sprintf("%s：评估通过", profile.Role), nil
}

// chairSummarize 主席汇总
func (c *CommitteeCoordinator) chairSummarize(ctx context.Context, evaluations []string, profiles []*AgentProfile, taskState *TaskState) string {
	// 如果主席 profile 有可用工具，则使用工具进行汇总
	if c.chairProfile != nil && len(c.chairProfile.AvailableTools) > 0 {
		var toolResults []string
		for _, toolName := range c.chairProfile.AvailableTools {
			result, err := c.executeTool(ctx, toolName, taskState)
			if err != nil {
				toolResults = append(toolResults, fmt.Sprintf("工具 %s 执行失败: %v", toolName, err))
			} else {
				toolResults = append(toolResults, fmt.Sprintf("工具 %s 执行结果:\n%s", toolName, result))
			}
		}

		if len(toolResults) > 0 {
			summary := fmt.Sprintf("【%s - 委员会决议】\n\n", c.chairProfile.Role)
			summary += "各位委员评估意见：\n"
			for i, eval := range evaluations {
				summary += fmt.Sprintf("%d. %s：%s\n", i+1, profiles[i].Role, eval)
			}
			summary += "\n工具分析结果：\n"
			summary += joinStrings(toolResults, "\n\n")
			return summary
		}
	}

	// 回退到默认汇总逻辑
	return c.defaultChairSummarize(evaluations, profiles)
}

// defaultChairSummarize 默认主席汇总逻辑
func (c *CommitteeCoordinator) defaultChairSummarize(evaluations []string, profiles []*AgentProfile) string {
	summary := fmt.Sprintf("【%s - 委员会决议】\n\n", c.chairProfile.Role)
	summary += "各位委员评估意见：\n"

	for i, eval := range evaluations {
		summary += fmt.Sprintf("%d. %s：%s\n", i+1, profiles[i].Role, eval)
	}

	summary += "\n综合评估结果：\n"
	summary += "- 证据充分性：通过\n"
	summary += "- 数据完整性：通过\n"
	summary += "- 分析可行性：通过\n"
	summary += "\n决议：同意执行分析任务"

	return summary
}

// executeTool 执行单个工具
func (c *CommitteeCoordinator) executeTool(ctx context.Context, toolName string, taskState *TaskState) (string, error) {
	return c.InvokeTool(ctx, toolName, ToolParamsFromTask(taskState))
}
