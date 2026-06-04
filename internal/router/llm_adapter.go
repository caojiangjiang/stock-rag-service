package router

import (
	"context"

	"stock_rag/internal/concurrency"

	"github.com/cloudwego/eino/schema"
)

// LLMClientAdapter 适配器：将 concurrency.LLMClient 适配为 router.LLMClient 接口
type LLMClientAdapter struct {
	llmClient *concurrency.LLMClient
}

// NewLLMClientAdapter 创建适配器
func NewLLMClientAdapter(llmClient *concurrency.LLMClient) *LLMClientAdapter {
	return &LLMClientAdapter{
		llmClient: llmClient,
	}
}

// Completion 实现 router.LLMClient 接口
func (a *LLMClientAdapter) Completion(ctx context.Context, messages []LLMMessage) (string, error) {
	var convMessages []*schema.Message
	for _, msg := range messages {
		convMessages = append(convMessages, &schema.Message{
			Role:    schema.RoleType(msg.Role),
			Content: msg.Content,
		})
	}

	response, err := a.llmClient.Generate(ctx, &concurrency.LLMRequest{
		Question: messages[len(messages)-1].Content,
		TaskType: "router_classifier",
		Messages: convMessages,
	})
	if err != nil {
		return "", err
	}

	return response, nil
}
