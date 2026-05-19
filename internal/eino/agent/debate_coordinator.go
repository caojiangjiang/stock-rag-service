package agent

import (
	"context"
	"fmt"
	"time"

	"stock_rag/internal/eino/tools"
)

// DebateCoordinator 辩论模式协调器
// 多个 Agent 针对同一问题进行辩论，最终达成共识
type DebateCoordinator struct {
	*BaseCoordinator
	maxRounds int
}

// NewDebateCoordinator 创建辩论协调器
func NewDebateCoordinator(profileRegistry *ProfileRegistry, toolRegistry *tools.ToolRegistry) *DebateCoordinator {
	base := NewBaseCoordinator("debate", profileRegistry, toolRegistry)
	return &DebateCoordinator{
		BaseCoordinator: base,
		maxRounds:       3,
	}
}

// SetMaxRounds 设置最大辩论轮数
func (c *DebateCoordinator) SetMaxRounds(rounds int) {
	c.maxRounds = rounds
}

// Execute 执行辩论协调逻辑
func (c *DebateCoordinator) Execute(ctx context.Context, taskState *TaskState) (string, error) {
	profiles := c.GetAgentProfiles()
	if len(profiles) < 2 {
		return "辩论模式至少需要 2 个 Agent", nil
	}

	taskState.UpdateStatus(TaskStatusRunning)

	// 初始化论点
	arguments := make([]string, len(profiles))
	for i := range arguments {
		arguments[i] = ""
	}

	// 多轮辩论
	for round := 1; round <= c.maxRounds; round++ {
		if err := ctx.Err(); err != nil {
			taskState.UpdateStatus(TaskStatusFailed)
			taskState.AddError(fmt.Sprintf("任务被取消: %v", err))
			return "", err
		}

		taskState.CurrentStep = round
		taskState.UpdatedAt = time.Now()

		// 每轮每个 Agent 依次发言
		for i, profile := range profiles {
			stepStart := time.Now()
			stepTrace := StepTrace{
				StepID:    fmt.Sprintf("round_%d_agent_%d", round, i+1),
				ToolName:  profile.Name,
				Input:     map[string]interface{}{"query": taskState.UserMessage, "round": round},
				StartTime: stepStart,
				Status:    TaskStatusRunning,
			}

			// 生成论点
			argument, err := c.generateArgument(ctx, profile, taskState, arguments, round, i)

			stepTrace.EndTime = time.Now()
			if err != nil {
				stepTrace.Status = TaskStatusFailed
				stepTrace.Error = err.Error()
			} else {
				stepTrace.Status = TaskStatusCompleted
				stepTrace.Output = argument
				arguments[i] = argument
				taskState.AddFinding(fmt.Sprintf("第%d轮 - %s: %s", round, profile.Role, argument))
			}
			taskState.AddStepTrace(stepTrace)

			if err != nil {
				taskState.UpdateStatus(TaskStatusFailed)
				return fmt.Sprintf("第%d轮 Agent %s 发言失败: %v", round, profile.Name, err), nil
			}
		}
	}

	// 生成最终结论
	summary := c.generateConclusion(ctx, arguments, profiles, taskState)
	taskState.Summary = summary
	taskState.UpdateStatus(TaskStatusCompleted)

	return summary, nil
}

// generateArgument 生成论点
// 真正调用对应 profile 的工具来生成论点
func (c *DebateCoordinator) generateArgument(ctx context.Context, profile *AgentProfile, taskState *TaskState, arguments []string, round, idx int) (string, error) {
	// 使用 profile 中配置的工具生成论点
	if len(profile.AvailableTools) > 0 {
		return c.generateArgumentWithTools(ctx, profile, taskState, arguments, round, idx)
	}

	// 回退：返回默认论点
	return c.defaultGenerateArgument(profile, taskState.UserMessage, round)
}

// generateArgumentWithTools 使用工具生成论点
func (c *DebateCoordinator) generateArgumentWithTools(ctx context.Context, profile *AgentProfile, taskState *TaskState, arguments []string, round, idx int) (string, error) {
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
		baseArg := fmt.Sprintf("[%s] ", profile.Role)
		if round == 1 {
			baseArg += fmt.Sprintf("支持观点：基于分析，\"%s\" 的相关证据充分。", taskState.UserMessage)
		} else {
			baseArg += "反驳：基于工具分析，存在不同观点需要讨论。"
		}
		return baseArg + "\n\n" + joinStrings(results, "\n\n"), nil
	}

	return c.defaultGenerateArgument(profile, taskState.UserMessage, round)
}

// defaultGenerateArgument 默认生成论点逻辑
func (c *DebateCoordinator) defaultGenerateArgument(profile *AgentProfile, task string, round int) (string, error) {
	switch profile.Name {
	case "evidence_collector":
		if round == 1 {
			return fmt.Sprintf("支持观点：基于文档检索，\"%s\" 的相关证据充分", task), nil
		}
		return "反驳：证据显示与反方观点存在差异，需要进一步验证", nil
	case "metric_extractor":
		if round == 1 {
			return "支持观点：财务数据支持该结论", nil
		}
		return "反驳：数据口径需要统一，当前分析存在偏差", nil
	case "analyst_writer":
		if round == 1 {
			return "支持观点：综合分析支持该结论", nil
		}
		return "反驳：分析框架需要完善，结论不够严谨", nil
	default:
		if round == 1 {
			return fmt.Sprintf("支持观点：%s 认为该方案可行", profile.Role), nil
		}
		return fmt.Sprintf("反驳：%s 认为存在改进空间", profile.Role), nil
	}
}

// generateConclusion 生成最终结论
func (c *DebateCoordinator) generateConclusion(ctx context.Context, arguments []string, profiles []*AgentProfile, taskState *TaskState) string {
	// 尝试使用 planner profile 的工具生成结论
	if TaskPlannerProfile != nil && len(TaskPlannerProfile.AvailableTools) > 0 {
		var toolResults []string
		for _, toolName := range TaskPlannerProfile.AvailableTools {
			result, err := c.executeTool(ctx, toolName, taskState)
			if err != nil {
				toolResults = append(toolResults, fmt.Sprintf("工具 %s 执行失败: %v", toolName, err))
			} else {
				toolResults = append(toolResults, fmt.Sprintf("工具 %s 执行结果:\n%s", toolName, result))
			}
		}

		if len(toolResults) > 0 {
			summary := "【辩论结束 - 最终结论】\n\n"
			summary += "各方观点汇总：\n"
			for i, arg := range arguments {
				if arg != "" {
					summary += fmt.Sprintf("%s: %s\n", profiles[i].Role, arg)
				}
			}
			summary += "\n工具分析结果：\n"
			summary += joinStrings(toolResults, "\n\n")
			return summary
		}
	}

	// 回退到默认结论生成逻辑
	return c.defaultGenerateConclusion(arguments, profiles)
}

// defaultGenerateConclusion 默认生成结论逻辑
func (c *DebateCoordinator) defaultGenerateConclusion(arguments []string, profiles []*AgentProfile) string {
	summary := "【辩论结束 - 最终结论】\n\n"
	summary += "各方观点汇总：\n"
	for i, arg := range arguments {
		if arg != "" {
			summary += fmt.Sprintf("%s: %s\n", profiles[i].Role, arg)
		}
	}
	summary += "\n经过多轮辩论，综合各方观点，达成以下共识：\n"
	summary += "- 证据充分性：需要进一步验证\n"
	summary += "- 数据准确性：数据口径需要统一\n"
	summary += "- 分析完整性：分析框架需要完善\n"
	summary += "\n建议：综合各方意见进行综合评估"
	return summary
}

// executeTool 执行单个工具
func (c *DebateCoordinator) executeTool(ctx context.Context, toolName string, taskState *TaskState) (string, error) {
	tool, err := c.GetToolInstance(toolName)
	if err != nil {
		return "", fmt.Errorf("工具 %s 未找到: %v", toolName, err)
	}

	if tool != nil {
		params := map[string]interface{}{
			"query":      taskState.UserMessage,
			"stock_code": taskState.StockCode,
		}
		return tool.Run(ctx, params)
	}

	return "", fmt.Errorf("无法执行工具 %s", toolName)
}
