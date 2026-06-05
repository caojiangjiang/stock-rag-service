package agent

import (
	"context"
	"fmt"
	"time"

	"stock_rag/internal/eino/adapter"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

type PlanCoordinator struct {
	*BaseCoordinator
	supervisorProfile     *AgentProfile
	agentBuilder          *AgentBuilder
	checkPointStore       adk.CheckPointStore
	interruptSessionStore InterruptSessionStore
	fixedSteps            bool // true 时使用固定串行流水线（原 pipeline 模式）
}

func NewPlanCoordinator(
	profileRegistry *ProfileRegistry,
	agentBuilder *AgentBuilder,
	checkPointStore adk.CheckPointStore,
	interruptSessionStore InterruptSessionStore,
) *PlanCoordinator {
	base := NewBaseCoordinator("plan", profileRegistry, agentBuilder)
	return &PlanCoordinator{
		BaseCoordinator:       base,
		supervisorProfile:     TaskPlannerProfile,
		agentBuilder:          agentBuilder,
		checkPointStore:       checkPointStore,
		interruptSessionStore: interruptSessionStore,
	}
}

func (c *PlanCoordinator) SetAgentBuilder(builder *AgentBuilder) {
	c.agentBuilder = builder
}

// SetFixedSteps 启用固定串行步骤模式（原 pipeline 协调器行为）。
func (c *PlanCoordinator) SetFixedSteps(fixed bool) {
	c.fixedSteps = fixed
}

// FixedSteps 是否处于固定串行模式。
func (c *PlanCoordinator) FixedSteps() bool {
	return c.fixedSteps
}

func (c *PlanCoordinator) Execute(ctx context.Context, taskState *TaskState) (string, error) {
	if c.fixedSteps {
		return c.executeFixedSteps(ctx, taskState)
	}
	profiles := c.GetAgentProfiles()
	if len(profiles) == 0 {
		return "没有配置任何 Agent", nil
	}

	rt := RuntimeFromContext(ctx)
	if rt == nil {
		rt = NewCoordinatorRuntime(c.Name(), nil)
	}
	ctx, endSpan := StartCoordinatorSpan(ctx, c.Name(), taskState)
	defer endSpan()

	coordinatorStart := time.Now()
	defer func() {
		status := "success"
		if taskState.Status == TaskStatusFailed {
			status = "error"
		}
		classifier := taskState.ClassifierType
		if classifier == "" {
			classifier = "unknown"
		}
		RecordCoordinatorResult(c.Name(), classifier, status, time.Since(coordinatorStart).Seconds())
	}()

	runCtx, cancel := rt.DeriveContext(ctx)
	defer cancel()

	taskState.UpdateStatus(TaskStatusRunning)

	modelAdapter := adapter.NewEinoModelAdapter()

	planner, err := c.createPlanner(runCtx, modelAdapter)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("创建 Planner 失败: %v", err))
		return "", err
	}

	executor, err := c.createExecutor(runCtx, modelAdapter, profiles)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("创建 Executor 失败: %v", err))
		return "", err
	}

	maxIterations := 5
	if rt.Strategy.MaxSteps > 0 && rt.Strategy.MaxSteps < maxIterations {
		maxIterations = rt.Strategy.MaxSteps
	}

	planExecuteAgent, err := planexecute.New(runCtx, &planexecute.Config{
		Planner:       planner,
		Executor:      executor,
		MaxIterations: maxIterations,
	})
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("创建 PlanExecute Agent 失败: %v", err))
		return "", err
	}

	messages := UserMessagesFromTask(taskState)
	processResult, err := RunADKWithCheckpoint(
		runCtx, rt, taskState, planExecuteAgent, messages,
		c.checkPointStore, c.interruptSessionStore, CoordinatorTypePlan, false,
	)
	if awaiting, ok := AsAwaitingHuman(err); ok {
		content := ""
		if processResult != nil {
			content = processResult.Content
		}
		return content, awaiting
	}
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("PlanExecute 执行错误: %v", err))
		if processResult != nil {
			return processResult.Content, err
		}
		return "", err
	}

	content := ""
	if processResult != nil {
		content = processResult.Content
	}
	taskState.UpdateStatus(TaskStatusCompleted)
	taskState.Summary = content
	return content, nil
}

func (c *PlanCoordinator) Resume(ctx context.Context, taskState *TaskState, interruptID string, resumeData any) (string, error) {
	rt := RuntimeFromContext(ctx)
	if rt == nil {
		rt = NewCoordinatorRuntime(c.Name(), nil)
	}
	ctx, endSpan := StartCoordinatorSpan(ctx, c.Name(), taskState)
	defer endSpan()

	runCtx, cancel := rt.DeriveContext(ctx)
	defer cancel()

	if c.fixedSteps {
		return c.resumeFixedSteps(runCtx, rt, ctx, taskState, interruptID, resumeData)
	}

	profiles := c.GetAgentProfiles()
	modelAdapter := adapter.NewEinoModelAdapter()
	planner, err := c.createPlanner(runCtx, modelAdapter)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		return "", err
	}
	executor, err := c.createExecutor(runCtx, modelAdapter, profiles)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		return "", err
	}
	maxIterations := 5
	if rt.Strategy.MaxSteps > 0 && rt.Strategy.MaxSteps < maxIterations {
		maxIterations = rt.Strategy.MaxSteps
	}
	planExecuteAgent, err := planexecute.New(runCtx, &planexecute.Config{
		Planner:       planner,
		Executor:      executor,
		MaxIterations: maxIterations,
	})
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		return "", err
	}

	processResult, err := ResumeADKWithCheckpoint(
		runCtx, rt, taskState, planExecuteAgent, c.checkPointStore, c.interruptSessionStore,
		CoordinatorTypePlan, interruptID, resumeData, false,
	)
	if awaiting, ok := AsAwaitingHuman(err); ok {
		content := ""
		if processResult != nil {
			content = processResult.Content
		}
		return content, awaiting
	}
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		if processResult != nil {
			return processResult.Content, err
		}
		return "", err
	}
	content := ""
	if processResult != nil {
		content = processResult.Content
	}
	taskState.UpdateStatus(TaskStatusCompleted)
	taskState.Summary = content
	return content, nil
}

func (c *PlanCoordinator) createPlanner(ctx context.Context, modelAdapter *adapter.EinoModelAdapter) (adk.Agent, error) {
	plannerConfig := &planexecute.PlannerConfig{
		ChatModelWithFormattedOutput: modelAdapter,
		ToolInfo:                     &planexecute.PlanToolInfo,
	}

	return planexecute.NewPlanner(ctx, plannerConfig)
}

func (c *PlanCoordinator) createExecutor(ctx context.Context, modelAdapter *adapter.EinoModelAdapter, profiles []*AgentProfile) (adk.Agent, error) {
	tools := c.buildTools(profiles)

	executorConfig := &planexecute.ExecutorConfig{
		Model: modelAdapter,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: tools,
			},
		},
	}

	return planexecute.NewExecutor(ctx, executorConfig)
}

func (c *PlanCoordinator) buildTools(profiles []*AgentProfile) []tool.BaseTool {
	if c.agentBuilder == nil {
		return nil
	}

	var allTools []tool.BaseTool
	for _, profile := range profiles {
		tools, err := c.agentBuilder.BuildToolsForProfile(profile)
		if err != nil || len(tools) == 0 {
			continue
		}
		allTools = append(allTools, tools...)
	}

	return allTools
}

func (c *PlanCoordinator) executeFixedSteps(ctx context.Context, taskState *TaskState) (string, error) {
	profiles := c.GetAgentProfiles()
	if len(profiles) == 0 {
		return "没有配置任何 Agent", nil
	}

	rt := RuntimeFromContext(ctx)
	if rt == nil {
		rt = NewCoordinatorRuntime(c.Name(), nil)
	}
	ctx, endSpan := StartCoordinatorSpan(ctx, c.Name(), taskState)
	defer endSpan()

	coordinatorStart := time.Now()
	defer func() {
		status := "success"
		if taskState.Status == TaskStatusFailed {
			status = "error"
		}
		classifier := taskState.ClassifierType
		if classifier == "" {
			classifier = "unknown"
		}
		RecordCoordinatorResult(c.Name(), classifier, status, time.Since(coordinatorStart).Seconds())
	}()

	runCtx, cancel := rt.DeriveContext(ctx)
	defer cancel()

	taskState.UpdateStatus(TaskStatusRunning)

	subAgents, err := c.createSubAgents(runCtx, profiles)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("创建子 Agent 失败: %v", err))
		return "", err
	}

	sequentialAgent, err := adk.NewSequentialAgent(runCtx, &adk.SequentialAgentConfig{
		Name:        "plan_fixed_steps",
		Description: "固定串行步骤协调器，按顺序执行每个子 Agent",
		SubAgents:   subAgents,
	})
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("创建 Sequential Agent 失败: %v", err))
		return "", err
	}

	messages := UserMessagesFromTask(taskState)
	processResult, err := RunADKWithCheckpoint(
		runCtx, rt, taskState, sequentialAgent, messages,
		c.checkPointStore, c.interruptSessionStore, CoordinatorTypePlan, false,
	)
	if awaiting, ok := AsAwaitingHuman(err); ok {
		content := ""
		if processResult != nil {
			content = processResult.Content
		}
		return content, awaiting
	}
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("固定串行执行错误: %v", err))
		if processResult != nil {
			return processResult.Content, err
		}
		return "", err
	}

	content := ""
	if processResult != nil {
		content = processResult.Content
	}
	taskState.UpdateStatus(TaskStatusCompleted)
	taskState.Summary = content
	return content, nil
}

func (c *PlanCoordinator) resumeFixedSteps(runCtx context.Context, rt *CoordinatorRuntime, spanCtx context.Context, taskState *TaskState, interruptID string, resumeData any) (string, error) {
	_ = spanCtx
	profiles := c.GetAgentProfiles()
	subAgents, err := c.createSubAgents(runCtx, profiles)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		return "", err
	}
	sequentialAgent, err := adk.NewSequentialAgent(runCtx, &adk.SequentialAgentConfig{
		Name:        "plan_fixed_steps",
		Description: "固定串行步骤协调器，按顺序执行每个子 Agent",
		SubAgents:   subAgents,
	})
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		return "", err
	}

	processResult, err := ResumeADKWithCheckpoint(
		runCtx, rt, taskState, sequentialAgent, c.checkPointStore, c.interruptSessionStore,
		CoordinatorTypePlan, interruptID, resumeData, false,
	)
	if awaiting, ok := AsAwaitingHuman(err); ok {
		content := ""
		if processResult != nil {
			content = processResult.Content
		}
		return content, awaiting
	}
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		if processResult != nil {
			return processResult.Content, err
		}
		return "", err
	}
	content := ""
	if processResult != nil {
		content = processResult.Content
	}
	taskState.UpdateStatus(TaskStatusCompleted)
	taskState.Summary = content
	return content, nil
}

func (c *PlanCoordinator) createSubAgents(ctx context.Context, profiles []*AgentProfile) ([]adk.Agent, error) {
	agents := make([]adk.Agent, 0, len(profiles))
	for _, profile := range profiles {
		var agent adk.Agent
		var err error

		if c.agentBuilder != nil {
			agent, err = c.agentBuilder.Build(ctx, profile)
		} else {
			agent, err = c.createRealChatAgent(ctx, profile)
		}

		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return agents, nil
}

func (c *PlanCoordinator) createRealChatAgent(ctx context.Context, profile *AgentProfile) (adk.Agent, error) {
	instruction := fmt.Sprintf("%s\n\n%s", profile.Role, profile.RolePrompt)
	if len(profile.Constraints) > 0 {
		instruction += "\n\n约束：\n"
		for i, constraint := range profile.Constraints {
			instruction += fmt.Sprintf("%d. %s\n", i+1, constraint)
		}
	}

	modelAdapter := adapter.NewEinoModelAdapter()

	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        profile.Name,
		Description: profile.Role,
		Model:       modelAdapter,
		Instruction: instruction,
	})
}
