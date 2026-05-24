package agent

import (
	"context"

	"stock_rag/internal/observability"
	"stock_rag/internal/router"
	"stock_rag/internal/service"
)

// ModeAgentExecutor ModeAgent 的唯一入口执行器
// 负责将请求转发到 TaskAgentService，不涉及具体的工作流编排细节
type ModeAgentExecutor struct {
	taskAgentService *service.TaskAgentService
}

// NewModeAgentExecutor 创建 ModeAgent 执行器
// 顶层 executor 只知道注入了一个 ModeAgentExecutor，不知道内部实现细节
func NewModeAgentExecutor(taskAgentService *service.TaskAgentService) *ModeAgentExecutor {
	return &ModeAgentExecutor{
		taskAgentService: taskAgentService,
	}
}

func (e *ModeAgentExecutor) Name() string {
	return "mode_agent_executor"
}

func (e *ModeAgentExecutor) Mode() router.RouteMode {
	return router.ModeAgent
}

func (e *ModeAgentExecutor) Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error) {
	ctx, span := observability.StartSpan(ctx, "ModeAgentExecutor.Execute")
	defer span.End()

	observability.L().InfoCtx(ctx, "ModeAgentExecutor starting execution for ModeAgent")

	taskReq := &service.ComplexTaskRequest{
		ConversationID: req.ConversationID,
		MessageID:      req.MessageID,
		UserID:         req.UserID,
		UserMessage:    req.UserMessage,
		StockCode:      req.StockCode,
	}

	resp, err := e.taskAgentService.ExecuteComplexTask(ctx, taskReq)
	if err != nil {
		observability.L().ErrorCtx(ctx, "ModeAgentExecutor task execution failed", err)
		return nil, err
	}

	return &ExecuteResponse{
		MessageID:    resp.MessageID,
		Content:      resp.Content,
		Mode:         router.ModeAgent,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		LatencyMs:    resp.LatencyMs,
		Error:        resp.Error,
	}, nil
}
