package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/supervisor"

	"stock_rag/internal/eino/adapter"
)

type SupervisorCoordinator struct {
	*BaseCoordinator
	supervisorProfile     *AgentProfile
	agentBuilder          *AgentBuilder
	checkPointStore       adk.CheckPointStore
	interruptSessionStore InterruptSessionStore
}

func NewSupervisorCoordinator(
	profileRegistry *ProfileRegistry,
	agentBuilder *AgentBuilder,
	checkPointStore adk.CheckPointStore,
	interruptSessionStore InterruptSessionStore,
) *SupervisorCoordinator {
	base := NewBaseCoordinator("supervisor", profileRegistry, agentBuilder)
	return &SupervisorCoordinator{
		BaseCoordinator:       base,
		supervisorProfile:     TaskPlannerProfile,
		agentBuilder:          agentBuilder,
		checkPointStore:       checkPointStore,
		interruptSessionStore: interruptSessionStore,
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
		} else if taskState.Status == TaskStatusAwaitingHuman {
			status = "awaiting_human"
		}
		classifier := taskState.ClassifierType
		if classifier == "" {
			classifier = "unknown"
		}
		RecordCoordinatorResult(c.Name(), classifier, status, time.Since(coordinatorStart).Seconds())
	}()

	runCtx, cancel := rt.DeriveContext(ctx)
	defer cancel()

	sv, err := c.buildSupervisor(runCtx)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(err.Error())
		return "", err
	}

	taskState.UpdateStatus(TaskStatusRunning)
	processResult, err := RunADKWithCheckpoint(
		runCtx, rt, taskState, sv, UserMessagesFromTask(taskState),
		c.checkPointStore, c.interruptSessionStore, CoordinatorTypeSupervisor, taskState.OnChunk != nil,
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

func (c *SupervisorCoordinator) Resume(ctx context.Context, taskState *TaskState, interruptID string, resumeData any) (string, error) {
	rt := RuntimeFromContext(ctx)
	if rt == nil {
		rt = NewCoordinatorRuntime(c.Name(), nil)
	}
	ctx, endSpan := StartCoordinatorSpan(ctx, c.Name(), taskState)
	defer endSpan()

	runCtx, cancel := rt.DeriveContext(ctx)
	defer cancel()

	sv, err := c.buildSupervisor(runCtx)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		return "", err
	}

	processResult, err := ResumeADKWithCheckpoint(
		runCtx, rt, taskState, sv, c.checkPointStore, c.interruptSessionStore,
		CoordinatorTypeSupervisor, interruptID, resumeData, taskState.OnChunk != nil,
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

func (c *SupervisorCoordinator) buildSupervisor(ctx context.Context) (adk.Agent, error) {
	supervisorAgent, err := c.createSupervisorAgent(ctx)
	if err != nil {
		return nil, fmt.Errorf("创建 Supervisor Agent 失败: %w", err)
	}
	subAgents, err := c.createSubAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("创建子 Agent 失败: %w", err)
	}
	return supervisor.New(ctx, &supervisor.Config{
		Supervisor: supervisorAgent,
		SubAgents:  subAgents,
	})
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
		var agentInst adk.Agent
		var err error

		if c.agentBuilder != nil {
			agentInst, err = c.agentBuilder.Build(ctx, profile)
		} else {
			agentInst, err = c.createSubAgent(ctx, profile)
		}

		if err != nil {
			return nil, err
		}
		subAgents = append(subAgents, agentInst)
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

	modelAdapter := adapter.NewEinoModelAdapter()

	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        profile.Name,
		Description: profile.Role,
		Model:       modelAdapter,
		Instruction: instruction,
	})
}
