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
	systemPrompt := `你是一个专业的金融助手。请用清晰、结构化的 Markdown 格式回答用户问题：
- 使用 ### 作为小节标题（标题前后空一行）
- 使用有序列表（1. 2. 3.）或无序列表（-）分点说明，每条独占一行
- 重要提示用 **加粗**
- 段落之间空一行，不要把所有内容挤在同一段里`

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
		Stream:   req.OnChunk != nil,
		Messages: messages,
		OnChunk:  req.OnChunk,
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
