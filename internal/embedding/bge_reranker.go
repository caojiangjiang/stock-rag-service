package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"stock_rag/internal/pkg/retry"
	"strings"
	"time"
)

// BGEReranker 实现基于 BGE-reranker 的二阶段重排
type BGEReranker struct {
	apiURL    string
	timeout   time.Duration
	apiKey    string
	modelName string
}

// BGERerankerConfig 配置 BGE-reranker
type BGERerankerConfig struct {
	APIURL    string
	Timeout   time.Duration
	APIKey    string
	ModelName string
}

// NewBGEReranker 创建 BGE-reranker 实例
func NewBGEReranker(cfg BGERerankerConfig) *BGEReranker {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.ModelName == "" {
		cfg.ModelName = "BAAI/bge-reranker-v2-m3"
	}
	return &BGEReranker{
		apiURL:    cfg.APIURL,
		timeout:   cfg.Timeout,
		apiKey:    cfg.APIKey,
		modelName: cfg.ModelName,
	}
}

// RerankRequest 重排请求
type RerankRequest struct {
	Query      string   `json:"query"`
	Texts      []string `json:"texts"`
	TopN       int      `json:"top_n,omitempty"`
	Model      string   `json:"model,omitempty"`
	ReturnText bool     `json:"return_text,omitempty"`
}

// RerankResponse 重排响应
type RerankResponse struct {
	Results []RerankResult `json:"results"`
}

// RerankResult 单个重排结果
type RerankResult struct {
	Index int     `json:"index"`
	Score float64 `json:"score"`
	Text  string  `json:"text,omitempty"`
}

// Rerank 对候选文档进行重排（带重试机制）
func (r *BGEReranker) Rerank(ctx context.Context, query string, texts []string, topN int) ([]RerankResult, error) {
	if len(texts) == 0 {
		return []RerankResult{}, nil
	}

	req := RerankRequest{
		Query:      query,
		Texts:      texts,
		TopN:       topN,
		Model:      r.modelName,
		ReturnText: false,
	}

	var results []RerankResult
	err := retry.Do(ctx, retry.DefaultConfig, func() error {
		rerankResults, err := r.doRerank(ctx, req)
		if err != nil {
			results = rerankResults
			return err
		}
		results = rerankResults
		return nil
	})

	return results, err
}

// doRerank 执行单次重排请求
func (r *BGEReranker) doRerank(ctx context.Context, req RerankRequest) ([]RerankResult, error) {
	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", r.apiURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if r.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+r.apiKey)
	}

	client := &http.Client{
		Timeout: r.timeout,
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, &retry.TemporaryError{Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		// 5xx 错误通常是临时性的，重试
		if resp.StatusCode >= 500 {
			return nil, &retry.TemporaryError{Err: fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))}
		}
		// 4xx 错误通常是永久性的，不重试
		return nil, &retry.PermanentError{Err: fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))}
	}

	var result RerankResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Results, nil
}

// RerankWithThreshold 对候选文档进行重排并应用阈值过滤
func (r *BGEReranker) RerankWithThreshold(ctx context.Context, query string, texts []string, topN int, threshold float64) ([]RerankResult, error) {
	results, err := r.Rerank(ctx, query, texts, topN)
	if err != nil {
		return nil, err
	}

	if threshold > 0 {
		filtered := make([]RerankResult, 0, len(results))
		for _, res := range results {
			if res.Score >= threshold {
				filtered = append(filtered, res)
			}
		}
		return filtered, nil
	}

	return results, nil
}
