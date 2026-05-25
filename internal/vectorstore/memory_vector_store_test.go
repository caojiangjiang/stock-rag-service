package vectorstore

import (
	"context"
	"testing"

	appmodel "stock_rag/internal/model"
)

func TestMemoryVectorStoreUpsertAndSearch(t *testing.T) {
	store := NewMemoryVectorStore()
	ctx := context.Background()

	err := store.Upsert(ctx, []Record{
		{
			ID:      "chunk-1",
			Content: "贵州茅台经营现金流",
			Vector:  []float32{1, 0, 0},
			Citation: appmodel.Citation{
				Title:   "年报",
				DocType: "report",
			},
			Metadata: map[string]string{
				"stock_code": "600519",
				"doc_type":   "report",
			},
		},
	})
	if err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	results, err := store.Search(ctx, SearchRequest{
		QueryVector: []float32{1, 0, 0},
		Filter: Filter{
			StockCode: "600519",
			DocTypes:  []string{"report"},
		},
		TopK: 3,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results")
	}
}
