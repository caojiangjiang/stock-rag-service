package retrieval

import (
	"context"

	ragretriever "stock_rag/internal/eino/retriever"
	"stock_rag/internal/model"
)

// EvidenceRetriever 证据检索能力接口
type EvidenceRetriever interface {
	Retrieve(ctx context.Context, req model.RAGQueryRequest) ([]ragretriever.RetrievedChunk, error)
}

// EvidenceRetrievalService 证据检索服务
type EvidenceRetrievalService struct {
	retriever ragretriever.Retriever
}

// NewEvidenceRetrievalService 创建证据检索服务
func NewEvidenceRetrievalService(retriever ragretriever.Retriever) *EvidenceRetrievalService {
	return &EvidenceRetrievalService{
		retriever: retriever,
	}
}

// Retrieve 执行检索操作
func (s *EvidenceRetrievalService) Retrieve(ctx context.Context, req model.RAGQueryRequest) ([]ragretriever.RetrievedChunk, error) {
	return s.retriever.Retrieve(ctx, req)
}

// DocumentFilter 文档过滤选项
type DocumentFilter struct {
	StockCode string
	TimeRange string
	DocTypes  []string
	TopK      int
}

// BuildRequest 构建检索请求
func BuildRequest(question string, filter DocumentFilter) model.RAGQueryRequest {
	return model.RAGQueryRequest{
		Question:  question,
		StockCode: filter.StockCode,
		TimeRange: filter.TimeRange,
		DocTypes:  filter.DocTypes,
		TopK:      filter.TopK,
	}
}
