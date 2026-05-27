package agent

import (
	"context"
	"fmt"
	"time"

	"stock_rag/internal/eino/adapter"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type PipelineCoordinator struct {
	*BaseCoordinator
	agentBuilder *AgentBuilder
}

func NewPipelineCoordinator(profileRegistry *ProfileRegistry, agentBuilder *AgentBuilder) *PipelineCoordinator {
	base := NewBaseCoordinator("pipeline", profileRegistry, agentBuilder)
	return &PipelineCoordinator{
		BaseCoordinator: base,
		agentBuilder:    agentBuilder,
	}
}

func (c *PipelineCoordinator) SetAgentBuilder(builder *AgentBuilder) {
	c.agentBuilder = builder
}

func (c *PipelineCoordinator) Execute(ctx context.Context, taskState *TaskState) (string, error) {
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
		Name:        "pipeline_coordinator",
		Description: "串行流水线协调器，按顺序执行每个子 Agent",
		SubAgents:   subAgents,
	})
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("创建 Sequential Agent 失败: %v", err))
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

	iterator := sequentialAgent.Run(runCtx, input)
	finalResult, err := rt.ProcessADKIterator(runCtx, taskState, iterator)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("Pipeline 执行错误: %v", err))
		return finalResult, err
	}

	taskState.UpdateStatus(TaskStatusCompleted)
	taskState.Summary = finalResult

	return finalResult, nil
}

func (c *PipelineCoordinator) createSubAgents(ctx context.Context, profiles []*AgentProfile) ([]adk.Agent, error) {
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

func (c *PipelineCoordinator) createRealChatAgent(ctx context.Context, profile *AgentProfile) (adk.Agent, error) {
	instruction := fmt.Sprintf("%s\n\n%s", profile.Role, profile.RolePrompt)
	if len(profile.Constraints) > 0 {
		instruction += "\n\n约束：\n"
		for i, constraint := range profile.Constraints {
			instruction += fmt.Sprintf("%d. %s\n", i+1, constraint)
		}
	}

	// 使用 EinoModelAdapter 包装 llm.GetLLMClient()
	modelAdapter := adapter.NewEinoModelAdapter()

	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        profile.Name,
		Description: profile.Role,
		Model:       modelAdapter,
		Instruction: instruction,
	})
}
