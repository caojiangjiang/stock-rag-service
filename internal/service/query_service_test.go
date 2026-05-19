package service

import (
	"context"
	"testing"

	einomodel "stock_rag/internal/eino/model"
	ragretriever "stock_rag/internal/eino/retriever"
	"stock_rag/internal/embedding"
	"stock_rag/internal/llm"
	appmodel "stock_rag/internal/model"
	"stock_rag/internal/vectorstore"
)

type staticRetriever struct {
	chunks []ragretriever.RetrievedChunk
}

func (r staticRetriever) Retrieve(_ context.Context, _ appmodel.RAGQueryRequest) ([]ragretriever.RetrievedChunk, error) {
	return r.chunks, nil
}

func TestQueryServiceQuery(t *testing.T) {
	t.Setenv("ARK_API_KEY", "")
	t.Setenv("ARK_MODEL", "")

	// 初始化 LLMClient 单例
	llm.InitLLMClient(&einomodel.ChatModel{
		Config: einomodel.ChatConfig{
			Provider:  "ark",
			ModelEnv:  "ARK_MODEL",
			APIKeyEnv: "ARK_API_KEY",
		},
	}, 10, 2)

	svc, err := NewQueryServiceWithDependencies(context.Background(), QueryServiceDependencies{
		LLMClient: llm.GetLLMClient(),
		Retriever: staticRetriever{chunks: []ragretriever.RetrievedChunk{{
			Content: "最近公司公告显示其正推进重要项目。",
			Citation: appmodel.Citation{
				Title:     "测试公告",
				DocType:   "announcement",
				SourceURL: "memory://test/query",
				Published: "2026-03-11",
			},
		}}},
	})
	if err != nil {
		t.Fatalf("new query service: %v", err)
	}

	resp, err := svc.Query(context.Background(), appmodel.RAGQueryRequest{
		Question:  "这家公司最近有什么值得关注的信息？",
		StockCode: "300750",
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if resp.RetrievedCount == 0 {
		t.Fatal("expected retrieved count > 0")
	}

	if len(svc.Pipeline()) != 6 {
		t.Fatalf("expected 6 pipeline steps, got %d", len(svc.Pipeline()))
	}
}

func TestQueryServiceListDocuments(t *testing.T) {
	t.Setenv("ARK_API_KEY", "")
	t.Setenv("ARK_MODEL", "")

	// 初始化 LLMClient 单例
	llm.InitLLMClient(&einomodel.ChatModel{
		Config: einomodel.ChatConfig{
			Provider:  "ark",
			ModelEnv:  "ARK_MODEL",
			APIKeyEnv: "ARK_API_KEY",
		},
	}, 10, 2)

	svc, err := NewQueryServiceWithDependencies(context.Background(), QueryServiceDependencies{
		LLMClient: llm.GetLLMClient(),
	})
	if err != nil {
		t.Fatalf("new query service: %v", err)
	}

	listResp, err := svc.ListDocuments(context.Background())
	if err != nil {
		t.Fatalf("list documents: %v", err)
	}
	if listResp.TotalCount != 0 {
		t.Fatalf("expected total_count 0, got %d", listResp.TotalCount)
	}
	if len(listResp.Documents) != 0 {
		t.Fatalf("expected 0 documents, got %d", len(listResp.Documents))
	}
}

func TestQueryServiceImportDocumentsUsesUnifiedStore(t *testing.T) {
	t.Setenv("ARK_API_KEY", "")
	t.Setenv("ARK_MODEL", "")

	llm.InitLLMClient(&einomodel.ChatModel{
		Config: einomodel.ChatConfig{
			Provider:  "ark",
			ModelEnv:  "ARK_MODEL",
			APIKeyEnv: "ARK_API_KEY",
		},
	}, 10, 2)

	store := vectorstore.NewMemoryVectorStore()
	svc, err := NewQueryServiceWithDependencies(context.Background(), QueryServiceDependencies{
		LLMClient:          llm.GetLLMClient(),
		DocumentRepository: store,
		VectorStore:        store,
		Embedder:           embedding.NewSimpleEmbedder(),
	})
	if err != nil {
		t.Fatalf("new query service: %v", err)
	}

	importResp, err := svc.ImportDocuments(context.Background(), appmodel.DocumentsImportRequest{
		Documents: []appmodel.Document{{
			StockCode:   "600519",
			CompanyName: "贵州茅台",
			DocType:     "report",
			Title:       "贵州茅台2024年年度报告",
			SourceURL:   "memory://docs/600519/report/2024",
			Published:   "2025-03-01",
			Content:     "2024年贵州茅台实现营业收入和净利润持续增长。",
			Keywords:    []string{"年度报告", "净利润"},
		}},
	})
	if err != nil {
		t.Fatalf("import documents: %v", err)
	}
	if importResp.ImportedCount != 1 {
		t.Fatalf("expected imported_count 1, got %d", importResp.ImportedCount)
	}

	listResp, err := svc.ListDocuments(context.Background())
	if err != nil {
		t.Fatalf("list documents after import: %v", err)
	}
	if listResp.TotalCount != 1 {
		t.Fatalf("expected total_count 1, got %d", listResp.TotalCount)
	}
	if listResp.Documents[0].Title != "贵州茅台2024年年度报告" {
		t.Fatalf("unexpected imported title: %s", listResp.Documents[0].Title)
	}
}

func TestNewQueryServiceWithDependenciesUsesInjectedRetriever(t *testing.T) {
	t.Setenv("ARK_API_KEY", "")
	t.Setenv("ARK_MODEL", "")

	// 初始化 LLMClient 单例
	llm.InitLLMClient(&einomodel.ChatModel{
		Config: einomodel.ChatConfig{
			Provider:  "ark",
			ModelEnv:  "ARK_MODEL",
			APIKeyEnv: "ARK_API_KEY",
		},
	}, 10, 2)

	svc, err := NewQueryServiceWithDependencies(context.Background(), QueryServiceDependencies{
		LLMClient: llm.GetLLMClient(),
		Retriever: staticRetriever{
			chunks: []ragretriever.RetrievedChunk{{
				Content: "注入检索器返回的内容。",
				Citation: appmodel.Citation{
					Title:     "Injected Retriever Result",
					DocType:   "news",
					SourceURL: "memory://injected/1",
					Published: "2026-03-11",
				},
			}},
		},
	})
	if err != nil {
		t.Fatalf("new query service with dependencies: %v", err)
	}

	resp, err := svc.Query(context.Background(), appmodel.RAGQueryRequest{
		Question:  "最近有什么信息？",
		StockCode: "300750",
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(resp.Citations) != 1 {
		t.Fatalf("expected 1 citation, got %d", len(resp.Citations))
	}
	if resp.Citations[0].Title != "Injected Retriever Result" {
		t.Fatalf("expected injected retriever citation, got %s", resp.Citations[0].Title)
	}
}
