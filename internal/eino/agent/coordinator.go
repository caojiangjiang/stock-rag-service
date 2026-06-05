package agent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"

	"stock_rag/internal/eino/tools"
)

type Coordinator interface {
	Name() string
	Execute(ctx context.Context, taskState *TaskState) (string, error)
	GetAgentProfiles() []*AgentProfile
	SetAgentProfiles(profiles []*AgentProfile)
}

// ResumableCoordinator 支持 HITL 中断后恢复。
type ResumableCoordinator interface {
	Coordinator
	Resume(ctx context.Context, taskState *TaskState, interruptID string, resumeData any) (string, error)
}

type CoordinatorType string

const (
	CoordinatorTypeSupervisor CoordinatorType = "supervisor"
	CoordinatorTypePlan       CoordinatorType = "plan"
	CoordinatorTypeWorkflow   CoordinatorType = "workflow"
	CoordinatorTypeMultiAgent CoordinatorType = "multi_agent"
	CoordinatorTypeDeep       CoordinatorType = "deep"

	// Deprecated: 兼容旧 API，Create 时自动映射到上述类型。
	CoordinatorTypePipeline  CoordinatorType = "pipeline"  // -> plan (fixed steps)
	CoordinatorTypePeer      CoordinatorType = "peer"      // -> multi_agent (parallel)
	CoordinatorTypeDebate    CoordinatorType = "debate"    // -> multi_agent (debate)
	CoordinatorTypeCommittee CoordinatorType = "committee" // -> multi_agent (committee)
)

// CoordinatorCreateOptions 协调器创建选项（由 NormalizeCoordinatorType 解析）。
type CoordinatorCreateOptions struct {
	PlanFixedSteps     bool
	MultiAgentTopology MultiAgentTopology
}

// NormalizeCoordinatorType 将协调器类型规范化为 5 种核心类型及创建选项。
func NormalizeCoordinatorType(coordinatorType CoordinatorType) (CoordinatorType, CoordinatorCreateOptions) {
	opts := CoordinatorCreateOptions{}
	switch coordinatorType {
	case CoordinatorTypePipeline:
		return CoordinatorTypePlan, CoordinatorCreateOptions{PlanFixedSteps: true}
	case CoordinatorTypePeer:
		return CoordinatorTypeMultiAgent, CoordinatorCreateOptions{MultiAgentTopology: TopologyParallel}
	case CoordinatorTypeDebate:
		return CoordinatorTypeMultiAgent, CoordinatorCreateOptions{MultiAgentTopology: TopologyDebate}
	case CoordinatorTypeCommittee:
		return CoordinatorTypeMultiAgent, CoordinatorCreateOptions{MultiAgentTopology: TopologyCommittee}
	default:
		return coordinatorType, opts
	}
}

// IsDeprecatedCoordinatorType 是否为已废弃的协调器类型别名。
func IsDeprecatedCoordinatorType(coordinatorType CoordinatorType) bool {
	switch coordinatorType {
	case CoordinatorTypePipeline, CoordinatorTypePeer, CoordinatorTypeDebate, CoordinatorTypeCommittee:
		return true
	default:
		return false
	}
}

type CoordinatorFactory struct {
	profileRegistry       *ProfileRegistry
	agentBuilder          *AgentBuilder
	checkPointStore       adk.CheckPointStore
	interruptSessionStore InterruptSessionStore
}

func NewCoordinatorFactory(
	profileRegistry *ProfileRegistry,
	agentBuilder *AgentBuilder,
	checkPointStore adk.CheckPointStore,
	interruptSessionStore InterruptSessionStore,
) *CoordinatorFactory {
	if checkPointStore == nil {
		checkPointStore = NewInMemoryADKCheckPointStore()
	}
	if interruptSessionStore == nil {
		interruptSessionStore = NewInMemoryInterruptSessionStore()
	}
	return &CoordinatorFactory{
		profileRegistry:       profileRegistry,
		agentBuilder:          agentBuilder,
		checkPointStore:       checkPointStore,
		interruptSessionStore: interruptSessionStore,
	}
}

func (f *CoordinatorFactory) CheckPointStore() adk.CheckPointStore {
	return f.checkPointStore
}

func (f *CoordinatorFactory) InterruptSessionStore() InterruptSessionStore {
	return f.interruptSessionStore
}

func (f *CoordinatorFactory) Create(coordinatorType CoordinatorType) (Coordinator, error) {
	normalized, opts := NormalizeCoordinatorType(coordinatorType)

	switch normalized {
	case CoordinatorTypeSupervisor:
		return NewSupervisorCoordinator(f.profileRegistry, f.agentBuilder, f.checkPointStore, f.interruptSessionStore), nil
	case CoordinatorTypePlan:
		pc := NewPlanCoordinator(f.profileRegistry, f.agentBuilder, f.checkPointStore, f.interruptSessionStore)
		if opts.PlanFixedSteps {
			pc.SetFixedSteps(true)
		}
		return pc, nil
	case CoordinatorTypeWorkflow:
		return NewWorkflowCoordinator(f.profileRegistry, f.agentBuilder), nil
	case CoordinatorTypeMultiAgent:
		mc := NewMultiAgentCoordinator(f.profileRegistry, f.agentBuilder)
		if opts.MultiAgentTopology != "" {
			mc.SetTopology(opts.MultiAgentTopology)
		}
		return mc, nil
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
	agentBuilder    *AgentBuilder
}

func NewBaseCoordinator(name string, profileRegistry *ProfileRegistry, agentBuilder *AgentBuilder) *BaseCoordinator {
	return &BaseCoordinator{
		name:            name,
		profiles:        make([]*AgentProfile, 0),
		profileRegistry: profileRegistry,
		agentBuilder:    agentBuilder,
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
	if c.agentBuilder == nil {
		return nil, fmt.Errorf("agent builder not set")
	}
	registry := c.agentBuilder.GetToolRegistry()
	if registry == nil {
		return nil, fmt.Errorf("tool registry not set")
	}
	toolInfo, err := registry.GetInfo(toolName)
	if err != nil {
		return nil, err
	}
	return toolInfo.Instance, nil
}

// InvokeTool 通过 ToolRegistry 统一调用（超时 / 重试 / 熔断）。
func (c *BaseCoordinator) InvokeTool(ctx context.Context, toolName string, params map[string]interface{}) (string, error) {
	if c.agentBuilder == nil {
		return "", fmt.Errorf("agent builder not set")
	}
	registry := c.agentBuilder.GetToolRegistry()
	if registry == nil {
		return "", fmt.Errorf("tool registry not set")
	}
	return registry.Invoke(ctx, toolName, params)
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
