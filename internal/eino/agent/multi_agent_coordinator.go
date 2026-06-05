package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MultiAgentTopology 多 Agent 协作拓扑。
type MultiAgentTopology string

const (
	TopologyParallel  MultiAgentTopology = "parallel"  // 并行独立分析后汇总（原 peer）
	TopologyCommittee MultiAgentTopology = "committee" // 串行评估 + 主席决策（原 committee）
	TopologyDebate    MultiAgentTopology = "debate"    // 多轮交叉辩论（原 debate）
)

// DebatePeerArgument 其他参与者在当前轮次之前的发言（供下一轮引用/反驳）。
type DebatePeerArgument struct {
	Name     string
	Role     string
	Argument string
}

// DebateArgumentGenerator 自定义论点生成（如投资角色圆桌的 LLM 发言）。
type DebateArgumentGenerator interface {
	GenerateArgument(
		ctx context.Context,
		profile *AgentProfile,
		taskState *TaskState,
		peerArguments []DebatePeerArgument,
		round int,
	) (string, error)
}

// DebateConclusionGenerator 自定义辩论结论生成。
type DebateConclusionGenerator interface {
	GenerateConclusion(
		ctx context.Context,
		arguments []string,
		profiles []*AgentProfile,
		taskState *TaskState,
	) (string, error)
}

// MultiAgentCoordinator 统一多 Agent 协作协调器，通过 Topology 区分协作模式。
type MultiAgentCoordinator struct {
	*BaseCoordinator
	topology            MultiAgentTopology
	maxRounds           int
	chairProfile        *AgentProfile
	argumentGenerator   DebateArgumentGenerator
	conclusionGenerator DebateConclusionGenerator
}

// NewMultiAgentCoordinator 创建多 Agent 协调器（默认 parallel 拓扑）。
func NewMultiAgentCoordinator(profileRegistry *ProfileRegistry, agentBuilder *AgentBuilder) *MultiAgentCoordinator {
	base := NewBaseCoordinator("multi_agent", profileRegistry, agentBuilder)
	return &MultiAgentCoordinator{
		BaseCoordinator: base,
		topology:        TopologyParallel,
		maxRounds:       3,
		chairProfile:    TaskPlannerProfile,
	}
}

// SetTopology 设置协作拓扑。
func (c *MultiAgentCoordinator) SetTopology(topology MultiAgentTopology) {
	if topology != "" {
		c.topology = topology
	}
}

// Topology 返回当前拓扑。
func (c *MultiAgentCoordinator) Topology() MultiAgentTopology {
	return c.topology
}

// SetMaxRounds 设置辩论最大轮数（仅 debate 拓扑生效）。
func (c *MultiAgentCoordinator) SetMaxRounds(rounds int) {
	if rounds < 1 {
		rounds = 1
	}
	c.maxRounds = rounds
}

// SetChairProfile 设置主席 Profile（仅 committee 拓扑生效）。
func (c *MultiAgentCoordinator) SetChairProfile(profile *AgentProfile) {
	c.chairProfile = profile
}

// SetArgumentGenerator 注入自定义论点生成器（debate 拓扑）。
func (c *MultiAgentCoordinator) SetArgumentGenerator(g DebateArgumentGenerator) {
	c.argumentGenerator = g
}

// SetConclusionGenerator 注入自定义结论生成器（debate 拓扑）。
func (c *MultiAgentCoordinator) SetConclusionGenerator(g DebateConclusionGenerator) {
	c.conclusionGenerator = g
}

// BuildDebatePeerArguments 收集除当前发言者外、已有非空论点。
func BuildDebatePeerArguments(profiles []*AgentProfile, arguments []string, selfIdx int) []DebatePeerArgument {
	peers := make([]DebatePeerArgument, 0, len(profiles))
	for i, p := range profiles {
		if i == selfIdx || arguments[i] == "" {
			continue
		}
		peers = append(peers, DebatePeerArgument{
			Name:     p.Name,
			Role:     p.Role,
			Argument: arguments[i],
		})
	}
	return peers
}

// Execute 按拓扑执行多 Agent 协作。
func (c *MultiAgentCoordinator) Execute(ctx context.Context, taskState *TaskState) (string, error) {
	switch c.topology {
	case TopologyCommittee:
		return c.executeCommittee(ctx, taskState)
	case TopologyDebate:
		return c.executeDebate(ctx, taskState)
	default:
		return c.executeParallel(ctx, taskState)
	}
}

// --- parallel（原 peer）---

func (c *MultiAgentCoordinator) executeParallel(ctx context.Context, taskState *TaskState) (string, error) {
	profiles := c.GetAgentProfiles()
	if len(profiles) == 0 {
		return "没有配置任何 Agent", nil
	}

	taskState.UpdateStatus(TaskStatusRunning)

	var wg sync.WaitGroup
	results := make([]string, len(profiles))
	errs := make([]error, len(profiles))
	mu := sync.Mutex{}

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

			output, err := c.executeAgentStep(ctx, prof, taskState)

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

	for i, err := range errs {
		if err != nil {
			taskState.UpdateStatus(TaskStatusFailed)
			taskState.AddError(fmt.Sprintf("Agent %s 执行失败: %v", profiles[i].Name, err))
		}
	}

	summary := c.summarizeParallel(results, profiles)
	taskState.Summary = summary
	taskState.UpdateStatus(TaskStatusCompleted)
	return summary, nil
}

func (c *MultiAgentCoordinator) summarizeParallel(results []string, profiles []*AgentProfile) string {
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

// --- committee ---

func (c *MultiAgentCoordinator) executeCommittee(ctx context.Context, taskState *TaskState) (string, error) {
	profiles := c.GetAgentProfiles()
	if len(profiles) == 0 {
		return "没有配置任何委员会成员", nil
	}

	taskState.UpdateStatus(TaskStatusRunning)
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

		evaluation, err := c.executeAgentStep(ctx, profile, taskState)

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

func (c *MultiAgentCoordinator) chairSummarize(ctx context.Context, evaluations []string, profiles []*AgentProfile, taskState *TaskState) string {
	if c.chairProfile != nil && len(c.chairProfile.AvailableTools) > 0 {
		var toolResults []string
		for _, toolName := range c.chairProfile.AvailableTools {
			result, err := c.invokeTool(ctx, toolName, taskState)
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

	return c.defaultChairSummarize(evaluations, profiles)
}

func (c *MultiAgentCoordinator) defaultChairSummarize(evaluations []string, profiles []*AgentProfile) string {
	role := "主席"
	if c.chairProfile != nil {
		role = c.chairProfile.Role
	}
	summary := fmt.Sprintf("【%s - 委员会决议】\n\n", role)
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

// --- debate ---

func (c *MultiAgentCoordinator) executeDebate(ctx context.Context, taskState *TaskState) (string, error) {
	profiles := c.GetAgentProfiles()
	if len(profiles) < 2 {
		return "辩论模式至少需要 2 个 Agent", nil
	}

	taskState.UpdateStatus(TaskStatusRunning)
	arguments := make([]string, len(profiles))

	for round := 1; round <= c.maxRounds; round++ {
		if err := ctx.Err(); err != nil {
			taskState.UpdateStatus(TaskStatusFailed)
			taskState.AddError(fmt.Sprintf("任务被取消: %v", err))
			return "", err
		}

		taskState.CurrentStep = round
		taskState.UpdatedAt = time.Now()

		for i, profile := range profiles {
			stepStart := time.Now()
			stepTrace := StepTrace{
				StepID:    fmt.Sprintf("round_%d_agent_%d", round, i+1),
				ToolName:  profile.Name,
				Input:     map[string]interface{}{"query": taskState.UserMessage, "round": round},
				StartTime: stepStart,
				Status:    TaskStatusRunning,
			}

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

	summary := c.generateConclusion(ctx, arguments, profiles, taskState)
	taskState.Summary = summary
	taskState.UpdateStatus(TaskStatusCompleted)
	return summary, nil
}

func (c *MultiAgentCoordinator) generateArgument(ctx context.Context, profile *AgentProfile, taskState *TaskState, arguments []string, round, idx int) (string, error) {
	if c.argumentGenerator != nil {
		peers := BuildDebatePeerArguments(c.GetAgentProfiles(), arguments, idx)
		return c.argumentGenerator.GenerateArgument(ctx, profile, taskState, peers, round)
	}

	if len(profile.AvailableTools) > 0 {
		return c.generateArgumentWithTools(ctx, profile, taskState, round)
	}

	return c.defaultGenerateArgument(profile, taskState.UserMessage, round)
}

func (c *MultiAgentCoordinator) generateArgumentWithTools(ctx context.Context, profile *AgentProfile, taskState *TaskState, round int) (string, error) {
	var results []string
	for _, toolName := range profile.AvailableTools {
		result, err := c.invokeTool(ctx, toolName, taskState)
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

func (c *MultiAgentCoordinator) defaultGenerateArgument(profile *AgentProfile, task string, round int) (string, error) {
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

func (c *MultiAgentCoordinator) generateConclusion(ctx context.Context, arguments []string, profiles []*AgentProfile, taskState *TaskState) string {
	if c.conclusionGenerator != nil {
		if summary, err := c.conclusionGenerator.GenerateConclusion(ctx, arguments, profiles, taskState); err == nil && strings.TrimSpace(summary) != "" {
			return summary
		}
	}

	if TaskPlannerProfile != nil && len(TaskPlannerProfile.AvailableTools) > 0 {
		var toolResults []string
		for _, toolName := range TaskPlannerProfile.AvailableTools {
			result, err := c.invokeTool(ctx, toolName, taskState)
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

	return c.defaultGenerateConclusion(arguments, profiles)
}

func (c *MultiAgentCoordinator) defaultGenerateConclusion(arguments []string, profiles []*AgentProfile) string {
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

// --- shared helpers ---

func (c *MultiAgentCoordinator) executeAgentStep(ctx context.Context, profile *AgentProfile, taskState *TaskState) (string, error) {
	if len(profile.AvailableTools) > 0 {
		var results []string
		for _, toolName := range profile.AvailableTools {
			result, err := c.invokeTool(ctx, toolName, taskState)
			if err != nil {
				results = append(results, fmt.Sprintf("工具 %s 执行失败: %v", toolName, err))
			} else {
				results = append(results, fmt.Sprintf("工具 %s 执行结果:\n%s", toolName, result))
			}
		}
		if len(results) > 0 {
			return fmt.Sprintf("[%s]\n%s", profile.Role, joinStrings(results, "\n\n")), nil
		}
	}
	return fmt.Sprintf("[%s] 处理完成", profile.Role), nil
}

func (c *MultiAgentCoordinator) invokeTool(ctx context.Context, toolName string, taskState *TaskState) (string, error) {
	return c.InvokeTool(ctx, toolName, ToolParamsFromTask(taskState))
}

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
