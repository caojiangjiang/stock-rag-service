package vectorstore

import (
	"context"
	"strings"
	"sync"

	appmodel "stock_rag/internal/model"
)

// MemoryVectorStore 是内存实现的向量存储
// 用于在PostgreSQL连接失败时作为回退方案
type MemoryVectorStore struct {
	mu      sync.RWMutex
	records []Record
	docs    []appmodel.Document
}

// NewMemoryVectorStore 创建内存向量存储
func NewMemoryVectorStore() *MemoryVectorStore {
	return &MemoryVectorStore{
		records: make([]Record, 0),
		docs:    make([]appmodel.Document, 0),
	}
}

// Upsert 插入或更新向量记录
func (s *MemoryVectorStore) Upsert(ctx context.Context, records []Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, record := range records {
		s.records = append(s.records, record)

		// 同时更新文档列表
		doc := appmodel.Document{
			StockCode:   record.Metadata["stock_code"],
			CompanyName: record.Metadata["company_name"],
			DocType:     record.Metadata["doc_type"],
			Title:       record.Metadata["title"],
			SourceURL:   record.Metadata["source_url"],
			Published:   record.Metadata["published_at"],
			Content:     record.Content,
		}
		s.docs = append(s.docs, doc)
	}

	return nil
}

// Search 搜索向量
func (s *MemoryVectorStore) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]SearchResult, 0)

	// 简单实现：返回前N个记录
	count := 0
	for _, record := range s.records {
		if count >= req.TopK {
			break
		}

		// 应用过滤条件
		if req.Filter.StockCode != "" && record.Metadata["stock_code"] != req.Filter.StockCode {
			continue
		}

		if len(req.Filter.DocTypes) > 0 {
			hasMatch := false
			for _, docType := range req.Filter.DocTypes {
				if record.Metadata["doc_type"] == docType {
					hasMatch = true
					break
				}
			}
			if !hasMatch {
				continue
			}
		}

		result := SearchResult{
			Content: record.Content,
			Citation: appmodel.Citation{
				Title:        record.Metadata["title"],
				DocType:      record.Metadata["doc_type"],
				SourceURL:    record.Metadata["source_url"],
				Published:    record.Metadata["published_at"],
				PageNo:       0,
				SectionTitle: record.Metadata["section_title"],
			},
			Score: 0.0,
		}
		results = append(results, result)
		count++
	}

	return results, nil
}

// ListDocuments 返回当前内存中的全部文档
func (s *MemoryVectorStore) ListDocuments(ctx context.Context) ([]appmodel.Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	docs := make([]appmodel.Document, 0, len(s.docs))
	for _, doc := range s.docs {
		docs = append(docs, doc)
	}

	return docs, nil
}

// ListAvailableDocTypes 返回当前内存 store 中可用的 doc_type。
func (s *MemoryVectorStore) ListAvailableDocTypes(ctx context.Context, stockCode string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, doc := range s.docs {
		if strings.TrimSpace(stockCode) != "" && !strings.EqualFold(strings.TrimSpace(stockCode), "GENERAL") && !strings.EqualFold(doc.StockCode, stockCode) {
			continue
		}
		docType := strings.ToLower(strings.TrimSpace(doc.DocType))
		if docType == "" {
			continue
		}
		if _, exists := seen[docType]; exists {
			continue
		}
		seen[docType] = struct{}{}
		result = append(result, docType)
	}
	return result, nil
}
