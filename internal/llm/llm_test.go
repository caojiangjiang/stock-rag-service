package llm

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"

	"stock_rag/internal/concurrency"
	einomodel "stock_rag/internal/eino/model"
)

func TestInitLLMClient(t *testing.T) {
	// 测试初始化LLM客户端
	model := &einomodel.ChatModel{
		Config: einomodel.ChatConfig{
			Provider:  "ark",
			ModelEnv:  "ARK_MODEL",
			APIKeyEnv: "ARK_API_KEY",
		},
	}

	InitLLMClient(model, 10, 2)

	// 验证客户端
	client := GetLLMClient()
	if client == nil {
		t.Fatalf("LLM客户端初始化失败")
	}
}

func TestGetLLMClient(t *testing.T) {
	// 测试获取LLM客户端
	client := GetLLMClient()
	if client == nil {
		t.Fatalf("获取LLM客户端失败")
	}
}

func TestGenerate(t *testing.T) {
	// 测试生成功能
	client := GetLLMClient()
	if client == nil {
		t.Skipf("跳过测试，LLM客户端未初始化")
		return
	}

	ctx := context.Background()
	req := &concurrency.LLMRequest{
		RequestID: "test-request",
		Question:  "测试问题",
		Messages: []*schema.Message{
			{
				Role:    "user",
				Content: "测试问题",
			},
		},
		TaskType: "test",
		Priority: 1,
		Stream:   false,
	}

	response, err := client.Generate(ctx, req)
	if err != nil {
		// 由于API密钥可能无效，这里可能会失败，所以跳过
		t.Skipf("跳过测试，API调用失败: %v", err)
		return
	}

	// 验证响应
	if response == "" {
		t.Fatalf("响应为空")
	}
}
