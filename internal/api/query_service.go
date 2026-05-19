package api

import (
	"context"

	appmodel "stock_rag/internal/model"
)

// QueryService 描述 API 层对查询服务的最小依赖契约。
type QueryService interface {
	ImportDocuments(ctx context.Context, req appmodel.DocumentsImportRequest) (appmodel.DocumentsImportResponse, error)
	ListDocuments(ctx context.Context) (appmodel.DocumentsListResponse, error)
	Query(ctx context.Context, req appmodel.RAGQueryRequest) (appmodel.RAGQueryResponse, error)
	QueryStream(ctx context.Context, req appmodel.RAGQueryRequest, onChunk func(string) error) (appmodel.RAGQueryResponse, error)
}
