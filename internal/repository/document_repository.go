package repository

import (
	"context"

	appmodel "stock_rag/internal/model"
)

// DocumentRepository 描述当前阶段文档存储的最小能力。
type DocumentRepository interface {
	ListDocuments(ctx context.Context) ([]appmodel.Document, error)
}
