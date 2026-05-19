package agent

import (
	"context"

	"github.com/cloudwego/eino/schema"

	"stock_rag/internal/concurrency"
	"stock_rag/internal/router"
)

type ChatExecutor struct {
	name      string
	llmClient *concurrency.LLMClient
}

func NewChatExecutor(llmClient *concurrency.LLMClient) *ChatExecutor {
	return &ChatExecutor{
		name:      "chat_executor",
		llmClient: llmClient,
	}
}

func (e *ChatExecutor) Name() string {
	return e.name
}

func (e *ChatExecutor) Mode() router.RouteMode {
	return router.ModeChat
}

func (e *ChatExecutor) Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error) {
	systemPrompt := "你是一个专业的金融助手。请回答用户的问题。"

	messages := []*schema.Message{
		{
			Role:    "system",
			Content: systemPrompt,
		},
		{
			Role:    "user",
			Content: req.UserMessage,
		},
	}

	content, err := e.llmClient.Generate(ctx, &concurrency.LLMRequest{
		Question: req.UserMessage,
		TaskType: "chat",
		Stream:   false,
		Messages: messages,
	})
	if err != nil {
		return &ExecuteResponse{
			Error: err.Error(),
		}, err
	}

	return &ExecuteResponse{
		Content: content,
		Mode:    router.ModeChat,
	}, nil
}
