package adapter

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"stock_rag/internal/concurrency"
	"stock_rag/internal/llm"
)

// EinoModelAdapter 是 Eino ToolCallingChatModel 的适配器
// 它将 Eino ADK 的模型调用转换为通过 llm.GetLLMClient() 统一网关调用
type EinoModelAdapter struct {
	tools []*schema.ToolInfo
}

// NewEinoModelAdapter 创建 Eino model adapter
func NewEinoModelAdapter() *EinoModelAdapter {
	return &EinoModelAdapter{
		tools: make([]*schema.ToolInfo, 0),
	}
}

// Generate 生成响应
func (a *EinoModelAdapter) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	llmClient := llm.GetLLMClient()
	if llmClient == nil {
		return nil, nil
	}

	// 构建 LLMRequest
	llmReq := &concurrency.LLMRequest{
		Question: extractQuestion(input),
		Messages: input,
		TaskType: "agent",
		Stream:   false,
		Priority: 1,
	}

	// 调用 LLMClient
	result, err := llmClient.Generate(ctx, llmReq)
	if err != nil {
		return nil, err
	}

	return &schema.Message{
		Role:    schema.Assistant,
		Content: result,
	}, nil
}

// Stream 流式生成响应
func (a *EinoModelAdapter) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	llmClient := llm.GetLLMClient()
	if llmClient == nil {
		return nil, nil
	}

	// 使用 schema.Pipe 创建流式读写器
	sr, sw := schema.Pipe[*schema.Message](16)

	// 异步调用流式生成
	go func() {
		defer sw.Close()

		llmReq := &concurrency.LLMRequest{
			Question: extractQuestion(input),
			Messages: input,
			TaskType: "agent",
			Stream:   true,
			Priority: 1,
			OnChunk: func(chunk string) error {
				sw.Send(&schema.Message{
					Role:    schema.Assistant,
					Content: chunk,
				}, nil)
				return nil
			},
		}

		// 调用流式生成（忽略最终结果，通过 OnChunk 回调获取）
		llmClient.Generate(ctx, llmReq)
	}()

	return sr, nil
}

// WithTools 返回绑定工具的新实例
func (a *EinoModelAdapter) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return &EinoModelAdapter{
		tools: append([]*schema.ToolInfo(nil), tools...),
	}, nil
}

// GetTools 获取当前绑定的工具
func (a *EinoModelAdapter) GetTools() []*schema.ToolInfo {
	return a.tools
}

// extractQuestion 从消息列表中提取用户问题
func extractQuestion(messages []*schema.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == schema.User {
			return messages[i].Content
		}
	}
	return ""
}
