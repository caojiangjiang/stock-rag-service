package concurrency

import (
	"context"
	"fmt"

	einomodel "stock_rag/internal/eino/model"

	"github.com/cloudwego/eino/schema"
)

// Example 展示如何使用并发控制骨架
func Example(chatModel *einomodel.ChatModel) {
	// 创建LLM客户端，设置队列大小为100，最大并发为10
	client := NewLLMClient(chatModel, 100, 10)

	// 示例1：发送单个请求
	ctx := context.Background()
	messages := []*schema.Message{
		{
			Role:    "user",
			Content: "分析特斯拉股票的财务状况",
		},
	}
	// 创建 LLMRequest
	llmReq := &LLMRequest{
		RequestID: "example-req-1",
		Messages:  messages,
		TaskType:  "example",
		Priority:  0,
		Timeout:   0, // 使用默认超时
		Stream:    false,
		Metadata: map[string]string{
			"task": "分析特斯拉股票的财务状况",
		},
	}
	result, err := client.Generate(ctx, llmReq)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Result: %s\n", result)
	}

	// 示例2：获取状态
	stats := client.GetStats()
	fmt.Printf("Queue stats: %v\n", stats)
}
