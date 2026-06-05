package adapter

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"stock_rag/internal/concurrency"
	"stock_rag/internal/llm"
)

// EinoModelAdapter 是 Eino ToolCallingChatModel 的适配器。
// Agent 模式优先走 Ark 原生 ToolCalling（支持 transfer_to_agent），否则回退 LLMClient。
type EinoModelAdapter struct {
	tools []*schema.ToolInfo
}

// NewEinoModelAdapter 创建 Eino model adapter
func NewEinoModelAdapter() *EinoModelAdapter {
	return &EinoModelAdapter{
		tools: make([]*schema.ToolInfo, 0),
	}
}

func (a *EinoModelAdapter) resolveArkModel() (model.ToolCallingChatModel, error) {
	base := arkToolCallingModel()
	if base == nil {
		return nil, nil
	}
	if len(a.tools) == 0 {
		return base, nil
	}
	return base.WithTools(a.tools)
}

func arkToolCallingModel() model.ToolCallingChatModel {
	client := llm.GetLLMClient()
	if client == nil {
		return nil
	}
	cm := client.GetChatModel()
	if cm == nil {
		return nil
	}
	return cm.ArkToolCallingModel()
}

// Generate 生成响应
func (a *EinoModelAdapter) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	arkModel, err := a.resolveArkModel()
	if err != nil {
		return nil, err
	}
	if arkModel != nil {
		out, err := arkModel.Generate(ctx, input, opts...)
		if err != nil {
			return nil, err
		}
		return normalizeModelMessage(out), nil
	}

	llmClient := llm.GetLLMClient()
	if llmClient == nil {
		return nil, nil
	}

	llmReq := &concurrency.LLMRequest{
		Question: extractQuestion(input),
		Messages: input,
		TaskType: "agent",
		Stream:   false,
		Priority: 1,
	}

	result, err := llmClient.Generate(ctx, llmReq)
	if err != nil {
		return nil, err
	}

	return normalizeModelMessage(&schema.Message{
		Role:    schema.Assistant,
		Content: result,
	}), nil
}

// Stream 流式生成响应
func (a *EinoModelAdapter) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	arkModel, err := a.resolveArkModel()
	if err != nil {
		return nil, err
	}
	if arkModel != nil {
		return arkModel.Stream(ctx, input, opts...)
	}

	llmClient := llm.GetLLMClient()
	if llmClient == nil {
		return nil, nil
	}

	sr, sw := schema.Pipe[*schema.Message](16)

	go func() {
		defer sw.Close()

		llmReq := &concurrency.LLMRequest{
			Question: extractQuestion(input),
			Messages: input,
			TaskType: "agent",
			Stream:   true,
			Priority: 1,
			OnChunk: func(chunk string) error {
				msg := normalizeModelMessage(&schema.Message{
					Role:    schema.Assistant,
					Content: chunk,
				})
				sw.Send(msg, nil)
				return nil
			},
		}

		result, genErr := llmClient.Generate(ctx, llmReq)
		if genErr != nil {
			sw.Send(nil, genErr)
			return
		}
		if stringsOnlyToolCall(result) {
			msg := normalizeModelMessage(&schema.Message{
				Role:    schema.Assistant,
				Content: result,
			})
			if len(msg.ToolCalls) > 0 {
				sw.Send(msg, nil)
			}
		}
	}()

	return sr, nil
}

func stringsOnlyToolCall(result string) bool {
	return result != "" && functionCallBlockRe.MatchString(result)
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
