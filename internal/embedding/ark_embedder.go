package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"stock_rag/internal/pkg/retry"
	"stock_rag/internal/pkgctx"
)

const arkEmbedHTTPTimeout = 30 * time.Second

// ArkEmbedder 是基于 Ark 模型的嵌入器实现
type ArkEmbedder struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
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
		client: &http.Client{
			Timeout: arkEmbedHTTPTimeout,
		},
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

	var vector []float32
	err := retry.Do(ctx, retry.DefaultConfig, func() error {
		v, callErr := e.embedQueryOnce(ctx, text)
		if callErr != nil {
			return callErr
		}
		vector = v
		return nil
	})
	if err != nil {
		return nil, err
	}
	return vector, nil
}

func (e *ArkEmbedder) embedQueryOnce(ctx context.Context, text string) ([]float32, error) {
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

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, &retry.PermanentError{Err: fmt.Errorf("failed to marshal payload: %w", err)}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(data))
	if err != nil {
		return nil, &retry.PermanentError{Err: fmt.Errorf("failed to create request: %w", err)}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, &retry.TemporaryError{Err: fmt.Errorf("failed to send request: %w", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &retry.TemporaryError{Err: fmt.Errorf("failed to read response: %w", err)}
	}

	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return nil, &retry.TemporaryError{Err: fmt.Errorf("ark api status %d: %s", resp.StatusCode, string(body))}
	}
	if resp.StatusCode >= 400 {
		return nil, &retry.PermanentError{Err: fmt.Errorf("ark api status %d: %s", resp.StatusCode, string(body))}
	}

	var response map[string]interface{}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, &retry.PermanentError{Err: fmt.Errorf("failed to decode response: %w", err)}
	}

	if errData, ok := response["error"].(map[string]interface{}); ok {
		if message, ok := errData["message"].(string); ok {
			return nil, &retry.PermanentError{Err: fmt.Errorf("ark api error: %s", message)}
		}
		return nil, &retry.PermanentError{Err: fmt.Errorf("ark api error: unknown error")}
	}

	vector, ok := parseEmbeddingVector(response)
	if !ok {
		return nil, &retry.PermanentError{Err: fmt.Errorf("no embedding data returned or invalid format")}
	}
	return vector, nil
}

func parseEmbeddingVector(response map[string]interface{}) ([]float32, bool) {
	data, ok := response["data"]
	if !ok {
		return nil, false
	}

	if dataArray, ok := data.([]interface{}); ok {
		for _, item := range dataArray {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if vector, ok := itemMap["embedding"].([]interface{}); ok {
					return toFloat32Vector(vector), true
				}
			}
		}
	}

	if dataMap, ok := data.(map[string]interface{}); ok {
		if embedding, ok := dataMap["embedding"].([]interface{}); ok {
			return toFloat32Vector(embedding), true
		}
	}

	return nil, false
}

func toFloat32Vector(values []interface{}) []float32 {
	vector := make([]float32, len(values))
	for i, val := range values {
		if floatVal, ok := val.(float64); ok {
			vector[i] = float32(floatVal)
		}
	}
	return vector
}
