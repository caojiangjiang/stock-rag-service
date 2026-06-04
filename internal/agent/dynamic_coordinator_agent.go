package agent

import (
	"context"
	"fmt"

	einoagent "stock_rag/internal/eino/agent"
	"stock_rag/internal/service"
)

// DynamicCoordinatorAgent 按请求 CoordinatorType 动态创建协调器并执行。
type DynamicCoordinatorAgent struct {
	factory  *einoagent.CoordinatorFactory
	profiles []*einoagent.AgentProfile
}

// NewDynamicCoordinatorAgent 创建动态协调器 Agent。
func NewDynamicCoordinatorAgent(factory *einoagent.CoordinatorFactory, profiles []*einoagent.AgentProfile) *DynamicCoordinatorAgent {
	return &DynamicCoordinatorAgent{
		factory:  factory,
		profiles: profiles,
	}
}

// ExecuteComplexTask 实现 service.ComplexTaskExecutor。
func (a *DynamicCoordinatorAgent) ExecuteComplexTask(ctx context.Context, req *service.ComplexTaskExecuteRequest) (*service.ComplexTaskResponse, error) {
	if a.factory == nil {
		return &service.ComplexTaskResponse{
			MessageID: req.MessageID,
			Error:     "coordinator factory not configured",
		}, nil
	}

	coordType := einoagent.CoordinatorType(req.CoordinatorType)
	if coordType == "" {
		coordType = einoagent.CoordinatorTypeSupervisor
	}

	coordinator, err := a.factory.Create(coordType)
	if err != nil {
		return &service.ComplexTaskResponse{
			MessageID: req.MessageID,
			Error:     fmt.Sprintf("create coordinator %s: %v", coordType, err),
		}, nil
	}
	if len(a.profiles) > 0 {
		coordinator.SetAgentProfiles(a.profiles)
	}

	runner := einoagent.NewCoordinatorSupervisorAdapter(coordinator)
	return runner.ExecuteComplexTask(ctx, req)
}
