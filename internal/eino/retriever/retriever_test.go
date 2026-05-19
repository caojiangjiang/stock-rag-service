package retriever

import (
	"context"
	"strings"
	"testing"

	"stock_rag/internal/embedding"
	appmodel "stock_rag/internal/model"
	"stock_rag/internal/vectorstore"
)

type stubEmbedder struct{}

func (stubEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, 0, len(texts))
	for range texts {
		vectors = append(vectors, []float32{0.1, 0.2})
	}
	return vectors, nil
}

func (stubEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.9, 0.3}, nil
}

type stubVectorStore struct{}

func (stubVectorStore) Upsert(_ context.Context, _ []vectorstore.Record) error {
	return nil
}

func (stubVectorStore) Search(_ context.Context, req vectorstore.SearchRequest) ([]vectorstore.SearchResult, error) {
	if len(req.QueryVector) == 0 {
		return nil, nil
	}

	return []vectorstore.SearchResult{{
		Content: "来自向量检索的语义召回结果。",
		Citation: appmodel.Citation{
			Title:     "向量检索命中结果",
			DocType:   "announcement",
			SourceURL: "vector://search/result/1",
			Published: "2026-03-11",
		},
		Score: 0.99,
	}}, nil
}

type stubKeywordVectorStore struct{}

func (stubKeywordVectorStore) Upsert(_ context.Context, _ []vectorstore.Record) error {
	return nil
}

func (stubKeywordVectorStore) Search(_ context.Context, _ vectorstore.SearchRequest) ([]vectorstore.SearchResult, error) {
	return nil, nil
}

func (stubKeywordVectorStore) KeywordSearch(_ context.Context, req vectorstore.KeywordSearchRequest) ([]vectorstore.SearchResult, error) {
	if !strings.Contains(req.QueryText, "经营现金流") && !strings.Contains(req.QueryText, "现金流") {
		return nil, nil
	}
	return []vectorstore.SearchResult{{
		Content: "贵州茅台2024年经营活动产生的现金流量净额为884.26亿元。",
		Citation: appmodel.Citation{
			Title:     "贵州茅台2024年年度报告",
			DocType:   "report",
			SourceURL: "db://keyword/result/1",
			Published: "2025-03-01",
		},
		Score: 0.91,
	}}, nil
}

type stubAvailableDocTypesVectorStore struct{}

func (stubAvailableDocTypesVectorStore) Upsert(_ context.Context, _ []vectorstore.Record) error {
	return nil
}

func (stubAvailableDocTypesVectorStore) Search(_ context.Context, req vectorstore.SearchRequest) ([]vectorstore.SearchResult, error) {
	if len(req.Filter.DocTypes) == 1 && req.Filter.DocTypes[0] == "report" {
		return []vectorstore.SearchResult{{
			Content: "来自 report 类型的数据库检索结果。",
			Citation: appmodel.Citation{
				Title:     "报告类型降级命中",
				DocType:   "report",
				SourceURL: "db://vector/report-only",
				Published: "2026-03-11",
			},
			Score: 0.88,
		}}, nil
	}
	return nil, nil
}

func (stubAvailableDocTypesVectorStore) ListAvailableDocTypes(_ context.Context, stockCode string) ([]string, error) {
	if stockCode == "600519" {
		return []string{"report"}, nil
	}
	return nil, nil
}

type stubDocumentFallbackVectorStore struct{}

func (stubDocumentFallbackVectorStore) Upsert(_ context.Context, _ []vectorstore.Record) error {
	return nil
}

func (stubDocumentFallbackVectorStore) Search(_ context.Context, _ vectorstore.SearchRequest) ([]vectorstore.SearchResult, error) {
	return nil, nil
}

func (stubDocumentFallbackVectorStore) FallbackDocumentSearch(_ context.Context, req vectorstore.KeywordSearchRequest) ([]appmodel.Document, error) {
	if req.Filter.StockCode != "600519" {
		return nil, nil
	}
	return []appmodel.Document{{
		StockCode:   "600519",
		CompanyName: "贵州茅台",
		DocType:     "report",
		Title:       "贵州茅台2024年年度报告",
		SourceURL:   "db://fallback/document/1",
		Published:   "2025-03-01",
		Content:     "贵州茅台2024年经营活动产生的现金流量净额为884.26亿元。",
		Keywords:    []string{"经营现金流", "年度报告"},
	}}, nil
}

var (
	_ embedding.Embedder                   = stubEmbedder{}
	_ vectorstore.VectorStore              = stubVectorStore{}
	_ vectorstore.KeywordSearcher          = stubKeywordVectorStore{}
	_ vectorstore.AvailableDocTypeLister   = stubAvailableDocTypesVectorStore{}
	_ vectorstore.DocumentFallbackSearcher = stubDocumentFallbackVectorStore{}
)

func TestLocalSampleRetrieverRetrieveFromTestdata(t *testing.T) {
	retriever := NewLocalSampleRetriever(nil)

	chunks, err := retriever.Retrieve(context.Background(), appmodel.RAGQueryRequest{
		Question:  "宁德时代港股上市进展怎么样？",
		StockCode: "300750",
		DocTypes:  []string{"announcement", "news"},
		TimeRange: "latest",
		TopK:      2,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected local sample chunks")
	}
	if strings.Contains(chunks[0].Citation.SourceURL, "example.com") {
		t.Fatalf("expected real local sample citation, got %s", chunks[0].Citation.SourceURL)
	}
	if !strings.Contains(chunks[0].Citation.Title, "上市") {
		t.Fatalf("expected sample title to mention 上市, got %s", chunks[0].Citation.Title)
	}
}

func TestLocalSampleRetrieverFallsBackWhenNoSampleMatches(t *testing.T) {
	retriever := NewLocalSampleRetriever(nil)

	chunks, err := retriever.Retrieve(context.Background(), appmodel.RAGQueryRequest{
		Question:  "这只股票最近有什么情况？",
		StockCode: "999999",
		DocTypes:  []string{"announcement"},
		TimeRange: "30d",
		TopK:      1,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected 0 fallback chunks, got %d", len(chunks))
	}
}

func TestHybridRetrieverUsesVectorStoreWhenConfigured(t *testing.T) {
	retriever := NewHybridRetriever(HybridRetrieverConfig{
		Embedder:    stubEmbedder{},
		VectorStore: stubVectorStore{},
	})

	chunks, err := retriever.Retrieve(context.Background(), appmodel.RAGQueryRequest{
		Question:  "这家公司最近有哪些重要公告？",
		StockCode: "300750",
		DocTypes:  []string{"announcement"},
		TimeRange: "30d",
		TopK:      1,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 vector chunk, got %d", len(chunks))
	}
	if chunks[0].Citation.Title != "向量检索命中结果" {
		t.Fatalf("expected vector search result, got %s", chunks[0].Citation.Title)
	}
}

func TestHybridRetrieverDoesNotMixLocalSamplesByDefault(t *testing.T) {
	retriever := NewHybridRetriever(HybridRetrieverConfig{})

	chunks, err := retriever.Retrieve(context.Background(), appmodel.RAGQueryRequest{
		Question:  "宁德时代港股上市进展怎么样？",
		StockCode: "300750",
		DocTypes:  []string{"announcement", "news"},
		TimeRange: "latest",
		TopK:      1,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected no local sample chunks in default production retriever, got %d", len(chunks))
	}
}

func TestHybridRetrieverUseLocalOnlySkipsVectorStore(t *testing.T) {
	retriever := NewHybridRetriever(HybridRetrieverConfig{
		Embedder:                 stubEmbedder{},
		VectorStore:              stubVectorStore{},
		LoadLocalSampleDocuments: true,
	})

	chunks, err := retriever.Retrieve(context.Background(), appmodel.RAGQueryRequest{
		Question:     "宁德时代港股上市进展怎么样？",
		StockCode:    "300750",
		DocTypes:     []string{"announcement", "news"},
		TimeRange:    "latest",
		TopK:         1,
		UseLocalOnly: true,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 local-only chunk, got %d", len(chunks))
	}
	if chunks[0].Citation.Title == "向量检索命中结果" {
		t.Fatalf("expected local-only mode to skip vector result")
	}
	if !strings.Contains(chunks[0].Citation.Title, "上市") {
		t.Fatalf("expected local sample title to mention 上市, got %s", chunks[0].Citation.Title)
	}
}

func TestHybridRetrieverUsesDatabaseKeywordSearchWhenAvailable(t *testing.T) {
	retriever := NewHybridRetriever(HybridRetrieverConfig{
		VectorStore: stubKeywordVectorStore{},
	})

	chunks, err := retriever.Retrieve(context.Background(), appmodel.RAGQueryRequest{
		Question:  "贵州茅台2024年经营现金流是多少？",
		StockCode: "600519",
		DocTypes:  []string{"report"},
		TopK:      1,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 keyword chunk, got %d", len(chunks))
	}
	if chunks[0].Citation.SourceURL != "db://keyword/result/1" {
		t.Fatalf("expected database keyword result, got %s", chunks[0].Citation.SourceURL)
	}
}

func TestHybridRetrieverGetsAvailableDocTypesFromVectorStore(t *testing.T) {
	retriever := NewHybridRetriever(HybridRetrieverConfig{
		Embedder:    stubEmbedder{},
		VectorStore: stubAvailableDocTypesVectorStore{},
	})

	chunks, err := retriever.Retrieve(context.Background(), appmodel.RAGQueryRequest{
		Question:  "贵州茅台最近有什么资料？",
		StockCode: "600519",
		TopK:      1,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 downgraded report chunk, got %d", len(chunks))
	}
	if chunks[0].Citation.Title != "报告类型降级命中" {
		t.Fatalf("expected report-only downgraded result, got %s", chunks[0].Citation.Title)
	}
}

func TestHybridRetrieverUsesDatabaseDocumentFallbackWhenMergedRetrievalMisses(t *testing.T) {
	retriever := NewHybridRetriever(HybridRetrieverConfig{
		VectorStore: stubDocumentFallbackVectorStore{},
	})

	chunks, err := retriever.Retrieve(context.Background(), appmodel.RAGQueryRequest{
		Question:  "贵州茅台2024年经营现金流是多少？",
		StockCode: "600519",
		DocTypes:  []string{"report"},
		TopK:      1,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 fallback document chunk, got %d", len(chunks))
	}
	if chunks[0].Citation.SourceURL != "db://fallback/document/1" {
		t.Fatalf("expected database document fallback result, got %s", chunks[0].Citation.SourceURL)
	}
}

func TestAnalyzeQueryUnderstandsFiscalYearAndMetric(t *testing.T) {
	intent := AnalyzeQuery(appmodel.RAGQueryRequest{Question: "贵州茅台2025年盈利怎么样？"})
	if intent.FiscalYear != 2025 {
		t.Fatalf("expected fiscal year 2025, got %d", intent.FiscalYear)
	}
	if intent.Metric != "净利润" {
		t.Fatalf("expected metric 净利润, got %s", intent.Metric)
	}
}

func TestAnalyzeQueryUnderstandsEPSMetric(t *testing.T) {
	intent := AnalyzeQuery(appmodel.RAGQueryRequest{Question: "贵州茅台2024年每股收益是多少？"})
	if intent.FiscalYear != 2024 {
		t.Fatalf("expected fiscal year 2024, got %d", intent.FiscalYear)
	}
	if intent.Metric != "每股收益" {
		t.Fatalf("expected metric 每股收益, got %s", intent.Metric)
	}
}

func TestLocalSampleRetrieverRejectsCrossYearProfitEvidence(t *testing.T) {
	retriever := NewLocalSampleRetriever(nil)

	chunks, err := retriever.Retrieve(context.Background(), appmodel.RAGQueryRequest{
		Question:  "贵州茅台2025年盈利怎么样？",
		StockCode: "600519",
		DocTypes:  []string{"report", "news"},
		TopK:      3,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected no cross-year chunks, got %d", len(chunks))
	}
}

func TestLocalSampleRetrieverReturnsProfitEvidenceWhenYearMatches(t *testing.T) {
	retriever := NewLocalSampleRetriever(nil)

	chunks, err := retriever.Retrieve(context.Background(), appmodel.RAGQueryRequest{
		Question:  "贵州茅台2024年盈利怎么样？",
		StockCode: "600519",
		DocTypes:  []string{"report", "news"},
		TopK:      2,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected 2024 profit chunks")
	}
	if !strings.Contains(chunks[0].Citation.Title, "2024") {
		t.Fatalf("expected top chunk title to mention 2024, got %s", chunks[0].Citation.Title)
	}
}
