package agent

import (
	"context"
	"os"

	"stock_rag/internal/model"
	"stock_rag/internal/service"
)

type QueryServiceRetriever struct {
	queryService *service.QueryService
}

func NewQueryServiceRetriever(queryService *service.QueryService) *QueryServiceRetriever {
	return &QueryServiceRetriever{queryService: queryService}
}

func (r *QueryServiceRetriever) Retrieve(ctx context.Context, query string, filters *RetrieveFilters) ([]RetrieveResult, error) {
	// Feature Flag: 检查是否使用新的检索路径
	useNewPath := os.Getenv("RAG_CONVERGENCE") == "phase1" || os.Getenv("RAG_CONVERGENCE") == ""

	if useNewPath {
		// 新路径: 使用 RetrieveEvidence，避免双次 LLM
		req := model.RAGQueryRequest{
			Question:  query,
			StockCode: filters.StockCode,
			DocTypes:  []string{filters.DocType},
			TimeRange: filters.TimeRange,
			TopK:      5,
		}

		chunks, err := r.queryService.RetrieveEvidence(ctx, req)
		if err != nil {
			return nil, err
		}

		var results []RetrieveResult
		for _, chunk := range chunks {
			results = append(results, RetrieveResult{
				Content:   chunk.Content,
				StockCode: filters.StockCode,
				DocType:   chunk.Citation.DocType,
				Title:     chunk.Citation.Title,
				Score:     0,
			})
		}
		return results, nil
	}

	// 旧路径 (兼容): 使用 Query
	req := model.RAGQueryRequest{
		Question:  query,
		StockCode: filters.StockCode,
		TopK:      5,
	}

	resp, err := r.queryService.Query(ctx, req)
	if err != nil {
		return nil, err
	}

	var results []RetrieveResult
	for _, citation := range resp.Citations {
		results = append(results, RetrieveResult{
			Content:   citation.Content, // 现在从Citation获取内容
			StockCode: filters.StockCode,
			DocType:   citation.DocType,
			Title:     citation.Title,
			Score:     0,
		})
	}

	return results, nil
}
