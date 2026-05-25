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
	"github.com/cloudwego/eino/schema"
)

type PlanCoordinator struct {
	*BaseCoordinator
	supervisorProfile *AgentProfile
	agentBuilder      *AgentBuilder
}

func NewPlanCoordinator(profileRegistry *ProfileRegistry, agentBuilder *AgentBuilder) *PlanCoordinator {
	base := NewBaseCoordinator("plan", profileRegistry, nil)
	return &PlanCoordinator{
		BaseCoordinator:   base,
		supervisorProfile: TaskPlannerProfile,
		agentBuilder:      agentBuilder,
	}
}

func (c *PlanCoordinator) SetAgentBuilder(builder *AgentBuilder) {
	c.agentBuilder = builder
}

func (c *PlanCoordinator) Execute(ctx context.Context, taskState *TaskState) (string, error) {
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
		RecordCoordinatorResult(c.Name(), status, time.Since(coordinatorStart).Seconds())
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

	userContent := taskState.UserMessage
	if taskState.StockCode != "" {
		userContent += fmt.Sprintf("\n\n股票代码: %s", taskState.StockCode)
	}

	input := &adk.AgentInput{
		Messages: []adk.Message{
			{
				Role:    schema.User,
				Content: userContent,
			},
		},
		EnableStreaming: false,
	}

	iterator := planExecuteAgent.Run(runCtx, input)
	finalResult, err := rt.ProcessADKIterator(runCtx, taskState, iterator)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("PlanExecute 执行错误: %v", err))
		return finalResult, err
	}

	taskState.UpdateStatus(TaskStatusCompleted)
	taskState.Summary = finalResult

	return finalResult, nil
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
