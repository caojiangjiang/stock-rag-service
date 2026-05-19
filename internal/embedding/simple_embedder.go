package embedding

import (
	"context"
)

// SimpleEmbedder 是一个简单的嵌入器实现，用于测试和开发
type SimpleEmbedder struct {}

// NewSimpleEmbedder 创建一个新的简单嵌入器
func NewSimpleEmbedder() *SimpleEmbedder {
	return &SimpleEmbedder{}
}

// EmbedDocuments 生成多个文档的嵌入向量（简单实现）
func (e *SimpleEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range texts {
		// 生成简单的向量表示
		vector := make([]float32, 10)
		for j := range vector {
			vector[j] = float32(i*10 + j)
		}
		vectors[i] = vector
	}
	return vectors, nil
}

// EmbedQuery 生成单个查询的嵌入向量（简单实现）
func (e *SimpleEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	// 生成简单的向量表示
	vector := make([]float32, 10)
	for i := range vector {
		vector[i] = float32(i)
	}
	return vector, nil
}
