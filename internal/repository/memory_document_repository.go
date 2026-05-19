package repository

import (
	"context"
	"sync"

	appmodel "stock_rag/internal/model"
)

// MemoryDocumentRepository 是最小内存文档仓库实现。
type MemoryDocumentRepository struct {
	mu   sync.RWMutex
	docs []appmodel.Document
}

// NewMemoryDocumentRepository 创建内存仓库。
func NewMemoryDocumentRepository() *MemoryDocumentRepository {
	return &MemoryDocumentRepository{docs: make([]appmodel.Document, 0)}
}



// ListDocuments 返回当前内存中的全部文档副本。
func (r *MemoryDocumentRepository) ListDocuments(_ context.Context) ([]appmodel.Document, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	docs := make([]appmodel.Document, 0, len(r.docs))
	for _, doc := range r.docs {
		docs = append(docs, cloneDocument(doc))
	}

	return docs, nil
}

func cloneDocument(doc appmodel.Document) appmodel.Document {
	cloned := doc
	if len(doc.Keywords) > 0 {
		cloned.Keywords = append([]string(nil), doc.Keywords...)
	}
	return cloned
}
