package embedding

import (
	"context"
	"testing"
)

func TestNewArkEmbedder(t *testing.T) {
	// 测试创建嵌入器
	ctx := context.Background()
	cfg := ArkEmbedderConfig{
		Provider:  "ark",
		Name:      "test-model",
		APIKeyEnv: "TEST_API_KEY",
		ModelEnv:  "TEST_MODEL",
		BaseURL:   "https://ark.cn-beijing.volces.com/api/v3",
	}

	// 设置环境变量
	t.Setenv("TEST_API_KEY", "test-api-key")
	t.Setenv("TEST_MODEL", "test-model")

	embedder, err := NewArkEmbedder(ctx, cfg)
	if err != nil {
		t.Fatalf("创建嵌入器失败: %v", err)
	}

	// 验证嵌入器
	if embedder.apiKey != "test-api-key" {
		t.Fatalf("API密钥不匹配")
	}

	if embedder.model != "test-model" {
		t.Fatalf("模型名称不匹配")
	}
}

func TestEmbedQuery(t *testing.T) {
	// 测试生成嵌入
	ctx := context.Background()
	cfg := ArkEmbedderConfig{
		Provider:  "ark",
		Name:      "test-model",
		APIKeyEnv: "TEST_API_KEY",
		ModelEnv:  "TEST_MODEL",
		BaseURL:   "https://ark.cn-beijing.volces.com/api/v3",
	}

	// 设置环境变量
	t.Setenv("TEST_API_KEY", "test-api-key")
	t.Setenv("TEST_MODEL", "test-model")

	embedder, err := NewArkEmbedder(ctx, cfg)
	if err != nil {
		t.Fatalf("创建嵌入器失败: %v", err)
	}

	text := "测试文本"

	embedding, err := embedder.EmbedQuery(ctx, text)
	if err != nil {
		// 由于API密钥是测试值，这里可能会失败，所以跳过
		t.Skipf("跳过测试，API密钥无效: %v", err)
		return
	}

	// 验证嵌入向量
	if len(embedding) == 0 {
		t.Fatalf("嵌入向量长度为0")
	}
}

func TestEmbedDocuments(t *testing.T) {
	// 测试批量生成嵌入
	ctx := context.Background()
	cfg := ArkEmbedderConfig{
		Provider:  "ark",
		Name:      "test-model",
		APIKeyEnv: "TEST_API_KEY",
		ModelEnv:  "TEST_MODEL",
		BaseURL:   "https://ark.cn-beijing.volces.com/api/v3",
	}

	// 设置环境变量
	t.Setenv("TEST_API_KEY", "test-api-key")
	t.Setenv("TEST_MODEL", "test-model")

	embedder, err := NewArkEmbedder(ctx, cfg)
	if err != nil {
		t.Fatalf("创建嵌入器失败: %v", err)
	}

	texts := []string{"测试文本1", "测试文本2"}

	embeddings, err := embedder.EmbedDocuments(ctx, texts)
	if err != nil {
		// 由于API密钥是测试值，这里可能会失败，所以跳过
		t.Skipf("跳过测试，API密钥无效: %v", err)
		return
	}

	// 验证嵌入向量
	if len(embeddings) != len(texts) {
		t.Fatalf("嵌入向量数量与文本数量不匹配")
	}

	for _, embedding := range embeddings {
		if len(embedding) == 0 {
			t.Fatalf("嵌入向量长度为0")
		}
	}
}
