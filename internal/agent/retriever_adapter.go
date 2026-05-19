package agent

import (
	"context"

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
