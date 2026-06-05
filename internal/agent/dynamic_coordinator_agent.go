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

// ResumeComplexTask 从 HITL 中断点恢复（支持 supervisor / plan）。
func (a *DynamicCoordinatorAgent) ResumeComplexTask(ctx context.Context, req *service.ResumeComplexTaskExecuteRequest) (*service.ComplexTaskResponse, error) {
	if a.factory == nil {
		return &service.ComplexTaskResponse{Error: "coordinator factory not configured"}, nil
	}
	if req == nil || req.CheckPointID == "" || req.InterruptID == "" {
		return &service.ComplexTaskResponse{Error: "checkpoint_id and interrupt_id are required"}, nil
	}

	session, err := a.factory.InterruptSessionStore().Get(ctx, req.CheckPointID)
	if err != nil {
		return &service.ComplexTaskResponse{Error: fmt.Sprintf("load interrupt session: %v", err)}, nil
	}

	coordType := einoagent.CoordinatorType(session.CoordinatorType)
	if coordType == "" {
		coordType = einoagent.CoordinatorTypeSupervisor
	}

	coordinator, err := a.factory.Create(coordType)
	if err != nil {
		return &service.ComplexTaskResponse{Error: fmt.Sprintf("create coordinator: %v", err)}, nil
	}
	if len(a.profiles) > 0 {
		coordinator.SetAgentProfiles(a.profiles)
	}

	resumable, ok := coordinator.(einoagent.ResumableCoordinator)
	if !ok {
		return &service.ComplexTaskResponse{Error: fmt.Sprintf("coordinator %s does not support resume", coordType)}, nil
	}

	taskState := einoagent.NewTaskState(session.ConversationID, session.MessageID, session.UserID, session.UserMessage)
	taskState.CheckPointID = session.CheckPointID
	taskState.StockCode = session.StockCode
	taskState.OnChunk = req.OnChunk

	rt := einoagent.NewCoordinatorRuntime(coordinator.Name(), nil)
	runCtx := einoagent.WithRuntime(ctx, rt)

	start := ctx
	result, err := resumable.Resume(runCtx, taskState, req.InterruptID, req.ResumeData)
	if awaiting, ok := einoagent.AsAwaitingHuman(err); ok {
		resp := &service.ComplexTaskResponse{
			Content:       result,
			AwaitingHuman: true,
		}
		if awaiting.Interrupt != nil {
			resp.CheckPointID = awaiting.Interrupt.CheckPointID
			resp.InterruptID = awaiting.Interrupt.ID
			resp.InterruptInfo = awaiting.Interrupt.Info
			resp.PartialContent = awaiting.Interrupt.PartialResult
		}
		_ = start
		return resp, nil
	}
	if err != nil {
		return &service.ComplexTaskResponse{Error: err.Error()}, nil
	}
	return &service.ComplexTaskResponse{Content: result}, nil
}
