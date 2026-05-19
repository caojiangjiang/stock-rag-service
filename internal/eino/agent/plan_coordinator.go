package agent

import (
	"context"
	"fmt"

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

	taskState.UpdateStatus(TaskStatusRunning)

	// 使用 EinoModelAdapter 包装 llm.GetLLMClient()
	// 调用链路: Eino ADK -> EinoModelAdapter -> llm.GetLLMClient() -> provider
	modelAdapter := adapter.NewEinoModelAdapter()

	planner, err := c.createPlanner(ctx, modelAdapter)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("创建 Planner 失败: %v", err))
		return "", err
	}

	executor, err := c.createExecutor(ctx, modelAdapter, profiles)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("创建 Executor 失败: %v", err))
		return "", err
	}

	planExecuteAgent, err := planexecute.New(ctx, &planexecute.Config{
		Planner:       planner,
		Executor:      executor,
		MaxIterations: 5,
	})
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("创建 PlanExecute Agent 失败: %v", err))
		return "", err
	}

	input := &adk.AgentInput{
		Messages: []adk.Message{
			{
				Role:    schema.User,
				Content: taskState.UserMessage,
			},
		},
		EnableStreaming: false,
	}

	iterator := planExecuteAgent.Run(ctx, input)

	var finalResult string
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}

		if event.Err != nil {
			taskState.UpdateStatus(TaskStatusFailed)
			taskState.AddError(fmt.Sprintf("PlanExecute 执行错误: %v", event.Err))
			return "", event.Err
		}

		if event.Output != nil && event.Output.MessageOutput != nil {
			msg, _ := event.Output.MessageOutput.GetMessage()
			finalResult += msg.Content
		}
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
