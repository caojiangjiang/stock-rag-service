package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"stock_rag/internal/pkgctx"
)

// ArkEmbedder 是基于 Ark 模型的嵌入器实现
type ArkEmbedder struct {
	apiKey  string
	model   string
	baseURL string
}

// ArkEmbedderConfig 描述 Ark 嵌入器的配置
type ArkEmbedderConfig struct {
	Provider  string
	Name      string
	APIKeyEnv string
	ModelEnv  string
	BaseURL   string
}

// DefaultArkEmbedderConfig 从 EmbeddingderConfig 创建默认的 ArkEmbedderConfig
func DefaultArkEmbedderConfig(embeddingderConfig pkgctx.EmbeddingderConfig) ArkEmbedderConfig {
	return ArkEmbedderConfig{
		Provider:  "ark",
		Name:      embeddingderConfig.Model,
		APIKeyEnv: embeddingderConfig.APIKeyEnv,
		ModelEnv:  embeddingderConfig.Model,
		BaseURL:   "https://ark.cn-beijing.volces.com/api/v3",
	}
}

// NewArkEmbedder 创建一个新的 Ark 嵌入器
func NewArkEmbedder(ctx context.Context, cfg ArkEmbedderConfig) (*ArkEmbedder, error) {
	apiKey := ""
	if cfg.APIKeyEnv != "" {
		apiKey = strings.TrimSpace(os.Getenv(cfg.APIKeyEnv))
	}

	model := strings.TrimSpace(cfg.Name)
	if model == "" && cfg.ModelEnv != "" {
		model = strings.TrimSpace(os.Getenv(cfg.ModelEnv))
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = "https://ark.cn-beijing.volces.com/api/v3"
	}

	if apiKey == "" || model == "" {
		return nil, fmt.Errorf("Ark embedder requires both API key and model name")
	}

	return &ArkEmbedder{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
	}, nil
}

// EmbedDocuments 生成多个文档的嵌入向量
func (e *ArkEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	if e == nil {
		return nil, fmt.Errorf("ark embedder not initialized")
	}

	vectors := make([][]float32, len(texts))
	for i, text := range texts {
		vector, err := e.EmbedQuery(ctx, text)
		if err != nil {
			return nil, err
		}
		vectors[i] = vector
	}

	return vectors, nil
}

// EmbedQuery 生成单个查询的嵌入向量
func (e *ArkEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	if e == nil {
		return nil, fmt.Errorf("ark embedder not initialized")
	}

	// 构建请求
	url := e.baseURL + "/embeddings/multimodal"
	payload := map[string]interface{}{
		"model": e.model,
		"input": []map[string]interface{}{
			{
				"type": "text",
				"text": text,
			},
		},
	}

	// 发送请求
	client := &http.Client{}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 解析响应
	var response map[string]interface{}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// 检查错误
	if errData, ok := response["error"].(map[string]interface{}); ok {
		if message, ok := errData["message"].(string); ok {
			return nil, fmt.Errorf("ark api error: %s", message)
		}
		return nil, fmt.Errorf("ark api error: unknown error")
	}

	// 尝试不同的数据结构
	if data, ok := response["data"]; ok {
		// 情况1: data 是数组
		if dataArray, ok := data.([]interface{}); ok {
			for _, item := range dataArray {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if embedding, ok := itemMap["embedding"].([]interface{}); ok {
						vector := make([]float32, len(embedding))
						for i, val := range embedding {
							if floatVal, ok := val.(float64); ok {
								vector[i] = float32(floatVal)
							}
						}
						return vector, nil
					}
				}
			}
		}
		// 情况2: data 是对象
		if dataMap, ok := data.(map[string]interface{}); ok {
			if embedding, ok := dataMap["embedding"].([]interface{}); ok {
				vector := make([]float32, len(embedding))
				for i, val := range embedding {
					if floatVal, ok := val.(float64); ok {
						vector[i] = float32(floatVal)
					}
				}
				return vector, nil
			}
		}
	}

	return nil, fmt.Errorf("no embedding data returned or invalid format")
}
