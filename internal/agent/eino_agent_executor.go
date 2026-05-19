package agent

import (
	"context"
	"fmt"

	"stock_rag/internal/concurrency"
	einoagent "stock_rag/internal/eino/agent"
	"stock_rag/internal/router"
)

type EinoAgentExecutor struct {
	name      string
	llmClient *concurrency.LLMClient
	tools     []einoagent.Tool
	config    einoagent.AgentConfig
}

func NewEinoAgentExecutor(llmClient *concurrency.LLMClient, tools []einoagent.Tool) *EinoAgentExecutor {
	config := einoagent.AgentConfig{
		LLMClient:   llmClient,
		Tools:       tools,
		MaxSteps:    10,
		Temperature: 0.7,
	}

	return &EinoAgentExecutor{
		name:      "eino_agent_executor",
		llmClient: llmClient,
		tools:     tools,
		config:    config,
	}
}

func (e *EinoAgentExecutor) Name() string {
	return e.name
}

func (e *EinoAgentExecutor) Mode() router.RouteMode {
	return router.ModeAgent
}

func (e *EinoAgentExecutor) Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error) {
	// 创建 AgentFactory
	factory := einoagent.NewAgentFactory(e.config)

	// 标准化用户问题中的实体（如"茅台" -> "贵州茅台" -> "600519"）
	taskCtx := factory.NormalizeTaskContext(req.UserMessage, nil)

	// 使用标准化后的上下文创建 Agent
	agentInstance := factory.CreateAgent(taskCtx)

	result, err := agentInstance.Run(ctx, req.UserMessage)
	if err != nil {
		return &ExecuteResponse{
			Error: fmt.Errorf("agent execution failed: %w", err).Error(),
			Mode:  router.ModeAgent,
		}, err
	}

	return &ExecuteResponse{
		Content: result,
		Mode:    router.ModeAgent,
	}, nil
}
