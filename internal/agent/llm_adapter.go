package agent

import (
	"context"

	"github.com/cloudwego/eino/schema"

	"stock_rag/internal/concurrency"
)

type LLMClientAdapter struct {
	client *concurrency.LLMClient
}

func NewLLMClientAdapter(client *concurrency.LLMClient) *LLMClientAdapter {
	return &LLMClientAdapter{client: client}
}

func (a *LLMClientAdapter) Generate(ctx context.Context, prompt string) (string, error) {
	messages := []*schema.Message{
		{
			Role:    "user",
			Content: prompt,
		},
	}
	req := &concurrency.LLMRequest{
		Question: prompt,
		TaskType: "chat",
		Stream:   false,
		Messages: messages,
	}
	return a.client.Generate(ctx, req)
}
