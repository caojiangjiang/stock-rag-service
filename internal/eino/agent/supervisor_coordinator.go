package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/supervisor"
	"github.com/cloudwego/eino/schema"
	"go.opentelemetry.io/otel/trace"

	"stock_rag/internal/eino/adapter"
)

type SupervisorCoordinator struct {
	*BaseCoordinator
	supervisorProfile *AgentProfile
	agentBuilder      *AgentBuilder
}

func NewSupervisorCoordinator(profileRegistry *ProfileRegistry, agentBuilder *AgentBuilder) *SupervisorCoordinator {
	base := NewBaseCoordinator("supervisor", profileRegistry, nil)
	return &SupervisorCoordinator{
		BaseCoordinator:   base,
		supervisorProfile: TaskPlannerProfile,
		agentBuilder:      agentBuilder,
	}
}

func (c *SupervisorCoordinator) SetSupervisorProfile(profile *AgentProfile) {
	c.supervisorProfile = profile
}

func (c *SupervisorCoordinator) GetSupervisorProfile() *AgentProfile {
	return c.supervisorProfile
}

func (c *SupervisorCoordinator) SetAgentBuilder(builder *AgentBuilder) {
	c.agentBuilder = builder
}

func (c *SupervisorCoordinator) Execute(ctx context.Context, taskState *TaskState) (string, error) {
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

	supervisorAgent, err := c.createSupervisorAgent(runCtx)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("创建 Supervisor Agent 失败: %v", err))
		return "", err
	}

	subAgents, err := c.createSubAgents(runCtx)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("创建子 Agent 失败: %v", err))
		return "", err
	}

	config := &supervisor.Config{
		Supervisor: supervisorAgent,
		SubAgents:  subAgents,
	}

	sv, err := supervisor.New(runCtx, config)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("创建 Supervisor 失败: %v", err))
		return "", err
	}

	taskState.UpdateStatus(TaskStatusRunning)

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

	iterator := sv.Run(runCtx, input)
	finalResult, err := rt.ProcessADKIterator(runCtx, taskState, iterator)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		return finalResult, err
	}

	taskState.UpdateStatus(TaskStatusCompleted)
	taskState.Summary = finalResult

	return finalResult, nil
}

func (c *SupervisorCoordinator) createSupervisorAgent(ctx context.Context) (adk.Agent, error) {
	instruction := `你是一位任务调度专家。你的职责是：

1. 分析用户的任务请求
2. 根据任务需求，选择合适的子 Agent 来执行
3. 协调多个子 Agent 完成复杂任务
4. 汇总子 Agent 的执行结果
5. 向用户提供最终的总结报告

可用的子 Agent：
- evidence_collector: 证据收集专家，检索文档证据
- metric_extractor: 财务指标专家，提取财务数据
- analyst_writer: 投资分析专家，生成分析报告

请根据任务性质，合理分配任务给相应的子 Agent。`

	// 使用 EinoModelAdapter 包装 llm.GetLLMClient()
	// 调用链路: Eino ADK -> EinoModelAdapter -> llm.GetLLMClient() -> provider
	modelAdapter := adapter.NewEinoModelAdapter()

	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        c.supervisorProfile.Name,
		Description: "任务调度专家，负责协调和管理子 Agent",
		Model:       modelAdapter,
		Instruction: instruction,
	})
}

func (c *SupervisorCoordinator) createSubAgents(ctx context.Context) ([]adk.Agent, error) {
	var subAgents []adk.Agent
	for _, profile := range c.GetAgentProfiles() {
		var agent adk.Agent
		var err error

		if c.agentBuilder != nil {
			agent, err = c.agentBuilder.Build(ctx, profile)
		} else {
			agent, err = c.createSubAgent(ctx, profile)
		}

		if err != nil {
			return nil, err
		}
		subAgents = append(subAgents, agent)
	}
	return subAgents, nil
}

func (c *SupervisorCoordinator) createSubAgent(ctx context.Context, profile *AgentProfile) (adk.Agent, error) {
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

func (c *SupervisorCoordinator) ExecuteWithCheckpoint(ctx context.Context, taskState *TaskState) (string, *InterruptInfo, error) {
	supervisorAgent, err := c.createSupervisorAgent(ctx)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("创建 Supervisor Agent 失败: %v", err))
		return "", nil, err
	}

	subAgents, err := c.createSubAgents(ctx)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("创建子 Agent 失败: %v", err))
		return "", nil, err
	}

	config := &supervisor.Config{
		Supervisor: supervisorAgent,
		SubAgents:  subAgents,
	}

	sv, err := supervisor.New(ctx, config)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("创建 Supervisor 失败: %v", err))
		return "", nil, err
	}

	taskState.UpdateStatus(TaskStatusRunning)
	taskState.GenerateCheckPointID()

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		EnableStreaming: false,
		Agent:           sv,
	})

	messages := []adk.Message{
		{
			Role:    schema.User,
			Content: taskState.UserMessage,
		},
	}

	iter := runner.Run(ctx, messages, adk.WithCheckPointID(taskState.CheckPointID))

	return c.processIteratorWithInterrupt(ctx, iter, taskState)
}

func (c *SupervisorCoordinator) processIteratorWithInterrupt(ctx context.Context, iter *adk.AsyncIterator[*adk.AgentEvent], taskState *TaskState) (string, *InterruptInfo, error) {
	var finalResult string
	var currentSpan trace.Span

	for {
		event, ok := iter.Next()
		if !ok {
			if currentSpan != nil {
				currentSpan.End()
			}
			break
		}

		if event.Err != nil {
			if currentSpan != nil {
				currentSpan.End()
			}
			taskState.UpdateStatus(TaskStatusFailed)
			taskState.AddError(fmt.Sprintf("Supervisor 执行错误: %v", event.Err))
			return "", nil, event.Err
		}

		if event.Action != nil && event.Action.Interrupted != nil {
			if currentSpan != nil {
				currentSpan.End()
			}
			taskState.CreateCheckpoint(event.Action.Interrupted.InterruptContexts[0].ID)

			interruptInfo := &InterruptInfo{
				ID:        event.Action.Interrupted.InterruptContexts[0].ID,
				Info:      event.Action.Interrupted.InterruptContexts[0].Info,
				Address:   event.Action.Interrupted.InterruptContexts[0].Address.String(),
				TaskState: taskState,
			}

			return finalResult, interruptInfo, nil
		}

		if event.Output != nil && event.Output.MessageOutput != nil {
			msg, _ := event.Output.MessageOutput.GetMessage()
			content := msg.Content
			finalResult += content

			agentName := event.AgentName
			if agentName == "" {
				agentName = "unknown_agent"
			}

			stepStartTime := time.Now()

			ctx, currentSpan = trace.SpanFromContext(ctx).TracerProvider().Tracer("supervisor_coordinator").Start(ctx, fmt.Sprintf("agent.%s", agentName))

			stepTrace := StepTrace{
				StepID:    fmt.Sprintf("%d", taskState.CurrentStep+1),
				ToolName:  agentName,
				Input:     map[string]interface{}{"query": taskState.UserMessage},
				Output:    content,
				StartTime: stepStartTime,
				EndTime:   time.Now(),
				Status:    TaskStatusCompleted,
			}

			stepTrace.LatencyMS = stepTrace.EndTime.Sub(stepTrace.StartTime).Milliseconds()

			taskState.AddStepTrace(stepTrace)
			taskState.CurrentStep++

			currentSpan.End()
		}

		if event.Action != nil && event.Action.TransferToAgent != nil {
			taskState.AddFinding(fmt.Sprintf("转移到 Agent: %s", event.Action.TransferToAgent.DestAgentName))
		}
	}

	taskState.UpdateStatus(TaskStatusCompleted)
	taskState.Summary = finalResult

	return finalResult, nil, nil
}

func (c *SupervisorCoordinator) ResumeFromCheckpoint(ctx context.Context, taskState *TaskState, interruptID string, resumeData any) (string, error) {
	supervisorAgent, err := c.createSupervisorAgent(ctx)
	if err != nil {
		return "", fmt.Errorf("创建 Supervisor Agent 失败: %v", err)
	}

	subAgents, err := c.createSubAgents(ctx)
	if err != nil {
		return "", fmt.Errorf("创建子 Agent 失败: %v", err)
	}

	config := &supervisor.Config{
		Supervisor: supervisorAgent,
		SubAgents:  subAgents,
	}

	sv, err := supervisor.New(ctx, config)
	if err != nil {
		return "", fmt.Errorf("创建 Supervisor 失败: %v", err)
	}

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		EnableStreaming: false,
		Agent:           sv,
	})

	params := &adk.ResumeParams{
		Targets: map[string]any{
			interruptID: resumeData,
		},
	}

	iter, err := runner.ResumeWithParams(ctx, taskState.CheckPointID, params)
	if err != nil {
		return "", fmt.Errorf("恢复执行失败: %v", err)
	}

	var finalResult string
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}

		if event.Err != nil {
			taskState.UpdateStatus(TaskStatusFailed)
			taskState.AddError(fmt.Sprintf("Supervisor 执行错误: %v", event.Err))
			return "", event.Err
		}

		if event.Output != nil && event.Output.MessageOutput != nil {
			msg, _ := event.Output.MessageOutput.GetMessage()
			if msg != nil {
				finalResult = msg.Content
				taskState.Summary = finalResult
			}
		}
	}

	taskState.UpdateStatus(TaskStatusCompleted)
	return finalResult, nil
}

type InterruptInfo struct {
	ID        string
	Info      any
	Address   string
	TaskState *TaskState
}
