package llm

import (
	"sync"
	"time"

	"stock_rag/internal/concurrency"
	einomodel "stock_rag/internal/eino/model"
)

var (
	llmClient *concurrency.LLMClient
	once      sync.Once
)

// InitLLMClient 初始化 LLMClient 单例
func InitLLMClient(chatModel *einomodel.ChatModel, maxQueueSize, maxConcurrency int) {
	InitLLMClientWithMaxWait(chatModel, maxQueueSize, maxConcurrency, 0)
}

// InitLLMClientWithMaxWait 初始化 LLMClient 单例，支持最大等待时长
func InitLLMClientWithMaxWait(chatModel *einomodel.ChatModel, maxQueueSize, maxConcurrency int, maxWaitTime time.Duration) {
	once.Do(func() {
		llmClient = concurrency.NewLLMClientWithMaxWait(chatModel, maxQueueSize, maxConcurrency, maxWaitTime)
	})
}

// GetLLMClient 获取 LLMClient 单例
func GetLLMClient() *concurrency.LLMClient {
	return llmClient
}

// Close 关闭 LLMClient
func Close() {
	if llmClient != nil {
		llmClient.Close()
	}
}
