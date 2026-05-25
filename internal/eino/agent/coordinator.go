package agent

import (
	"context"
	"fmt"

	"stock_rag/internal/eino/tools"
)

type Coordinator interface {
	Name() string
	Execute(ctx context.Context, taskState *TaskState) (string, error)
	GetAgentProfiles() []*AgentProfile
	SetAgentProfiles(profiles []*AgentProfile)
}

type CoordinatorType string

const (
	CoordinatorTypeSupervisor CoordinatorType = "supervisor"
	CoordinatorTypePipeline   CoordinatorType = "pipeline"
	CoordinatorTypeWorkflow   CoordinatorType = "workflow"
	CoordinatorTypePlan       CoordinatorType = "plan"
	CoordinatorTypePeer       CoordinatorType = "peer"
	CoordinatorTypeDebate     CoordinatorType = "debate"
	CoordinatorTypeCommittee  CoordinatorType = "committee"
	CoordinatorTypeDeep       CoordinatorType = "deep"
)

type CoordinatorFactory struct {
	profileRegistry *ProfileRegistry
	agentBuilder    *AgentBuilder
	toolRegistry    *tools.ToolRegistry
}

func NewCoordinatorFactory(profileRegistry *ProfileRegistry, agentBuilder *AgentBuilder, toolRegistry *tools.ToolRegistry) *CoordinatorFactory {
	return &CoordinatorFactory{
		profileRegistry: profileRegistry,
		agentBuilder:    agentBuilder,
		toolRegistry:    toolRegistry,
	}
}

func (f *CoordinatorFactory) Create(coordinatorType CoordinatorType) (Coordinator, error) {
	switch coordinatorType {
	case CoordinatorTypeSupervisor:
		return NewSupervisorCoordinator(f.profileRegistry, f.agentBuilder), nil
	case CoordinatorTypePipeline:
		return NewPipelineCoordinator(f.profileRegistry, f.agentBuilder), nil
	case CoordinatorTypeWorkflow:
		return NewWorkflowCoordinator(f.profileRegistry), nil
	case CoordinatorTypePlan:
		return NewPlanCoordinator(f.profileRegistry, f.agentBuilder), nil
	case CoordinatorTypePeer:
		return NewPeerCoordinator(f.profileRegistry, f.toolRegistry), nil
	case CoordinatorTypeDebate:
		return NewDebateCoordinator(f.profileRegistry, f.toolRegistry), nil
	case CoordinatorTypeCommittee:
		return NewCommitteeCoordinator(f.profileRegistry, f.toolRegistry), nil
	case CoordinatorTypeDeep:
		return NewDeepCoordinator(f.profileRegistry, f.agentBuilder), nil
	default:
		return nil, fmt.Errorf("未知的协调器类型: %s", coordinatorType)
	}
}

type BaseCoordinator struct {
	name            string
	profiles        []*AgentProfile
	profileRegistry *ProfileRegistry
	toolRegistry    *tools.ToolRegistry
}

func NewBaseCoordinator(name string, profileRegistry *ProfileRegistry, toolRegistry *tools.ToolRegistry) *BaseCoordinator {
	return &BaseCoordinator{
		name:            name,
		profiles:        make([]*AgentProfile, 0),
		profileRegistry: profileRegistry,
		toolRegistry:    toolRegistry,
	}
}

func (c *BaseCoordinator) Name() string {
	return c.name
}

func (c *BaseCoordinator) GetAgentProfiles() []*AgentProfile {
	return c.profiles
}

func (c *BaseCoordinator) SetAgentProfiles(profiles []*AgentProfile) {
	c.profiles = profiles
}

func (c *BaseCoordinator) GetProfileByName(name string) *AgentProfile {
	if c.profileRegistry == nil {
		return nil
	}
	profile, _ := c.profileRegistry.Get(name)
	return profile
}

func (c *BaseCoordinator) GetToolInstance(toolName string) (tools.Tool, error) {
	if c.toolRegistry == nil {
		return nil, fmt.Errorf("tool registry not set")
	}
	toolInfo, err := c.toolRegistry.GetInfo(toolName)
	if err != nil {
		return nil, err
	}
	return toolInfo.Instance, nil
}

// InvokeTool 通过 ToolRegistry 统一调用（超时 / 重试 / 熔断）。
func (c *BaseCoordinator) InvokeTool(ctx context.Context, toolName string, params map[string]interface{}) (string, error) {
	if c.toolRegistry == nil {
		return "", fmt.Errorf("tool registry not set")
	}
	return c.toolRegistry.Invoke(ctx, toolName, params)
}

// ToolParamsFromTask 从任务状态构造通用工具参数。
func ToolParamsFromTask(taskState *TaskState) map[string]interface{} {
	if taskState == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"query":      taskState.UserMessage,
		"stock_code": taskState.StockCode,
	}
}
