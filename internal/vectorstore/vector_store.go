package vectorstore

import (
	"context"

	appmodel "stock_rag/internal/model"
)

// Filter 描述向量检索前可应用的元数据过滤条件。
type Filter struct {
	StockCode string
	DocTypes  []string
	TimeRange string
}

// Record 表示一条准备写入向量库的记录。
type Record struct {
	ID       string
	Content  string
	Citation appmodel.Citation
	Vector   []float32
	Metadata map[string]string
}

// SearchRequest 描述一次向量检索请求。
type SearchRequest struct {
	QueryText           string
	QueryVector         []float32
	TopK                int
	Filter              Filter
	SimilarityThreshold float64 // 相似度阈值，低于此值的结果将被过滤
}

// KeywordSearchRequest 描述一次关键词/全文检索请求。
type KeywordSearchRequest struct {
	QueryText string
	Terms     []string
	TopK      int
	Filter    Filter
}

// SearchResult 表示向量检索命中的一条结果。
type SearchResult struct {
	Content  string
	Citation appmodel.Citation
	Score    float64
}

// VectorStore 描述向量库的最小能力。
type VectorStore interface {
	Upsert(ctx context.Context, records []Record) error
	Search(ctx context.Context, req SearchRequest) ([]SearchResult, error)
}

// KeywordSearcher 描述可选的数据库侧关键词检索能力。
type KeywordSearcher interface {
	KeywordSearch(ctx context.Context, req KeywordSearchRequest) ([]SearchResult, error)
}

// AvailableDocTypeLister 描述可选的数据库侧 doc_type 枚举能力。
type AvailableDocTypeLister interface {
	ListAvailableDocTypes(ctx context.Context, stockCode string) ([]string, error)
}

// DocumentFallbackSearcher 描述可选的数据库侧文档级 fallback 检索能力。
type DocumentFallbackSearcher interface {
	FallbackDocumentSearch(ctx context.Context, req KeywordSearchRequest) ([]appmodel.Document, error)
}
