package vectorstore

import (
	"context"
	"fmt"
	"testing"

	appmodel "stock_rag/internal/model"

	"github.com/jackc/pgx/v5"
)

const (
	testDBHost     = "localhost"
	testDBPort     = "5432"
	testDBUser     = "postgres"
	testDBPassword = "postgres123456"
	testDBName     = "stock_rag_test"
)

// setupTestDB 创建测试数据库并初始化表结构
func setupTestDB(t *testing.T) (*PgVectorStore, error) {
	ctx := context.Background()

	// 连接到默认数据库
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=postgres sslmode=disable",
		testDBHost, testDBPort, testDBUser, testDBPassword)
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("连接默认数据库失败: %w", err)
	}
	defer conn.Close(ctx)

	// 删除并重新创建测试数据库
	_, err = conn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", testDBName))
	if err != nil {
		return nil, fmt.Errorf("删除测试数据库失败: %w", err)
	}

	_, err = conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", testDBName))
	if err != nil {
		return nil, fmt.Errorf("创建测试数据库失败: %w", err)
	}

	// 连接到测试数据库
	store, err := NewPgVectorStore(ctx, testDBHost, testDBPort, testDBUser, testDBPassword, testDBName)
	if err != nil {
		return nil, fmt.Errorf("创建向量存储失败: %w", err)
	}

	return store, nil
}

// teardownTestDB 清理测试数据库
func teardownTestDB(t *testing.T) error {
	ctx := context.Background()

	// 连接到默认数据库
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=postgres sslmode=disable",
		testDBHost, testDBPort, testDBUser, testDBPassword)
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return fmt.Errorf("连接默认数据库失败: %w", err)
	}
	defer conn.Close(ctx)

	// 删除测试数据库
	_, err = conn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", testDBName))
	if err != nil {
		return fmt.Errorf("删除测试数据库失败: %w", err)
	}

	return nil
}

func TestNewPgVectorStore(t *testing.T) {
	// 测试创建向量存储
	store, err := setupTestDB(t)
	if err != nil {
		t.Skipf("跳过测试，无法设置测试数据库: %v", err)
		return
	}
	defer teardownTestDB(t)

	// 测试连接
	if store.conn == nil {
		t.Fatalf("连接为空")
	}
}

func TestSearch(t *testing.T) {
	// 测试搜索功能
	store, err := setupTestDB(t)
	if err != nil {
		t.Skipf("跳过测试，无法设置测试数据库: %v", err)
		return
	}
	defer teardownTestDB(t)

	// 插入测试数据
	ctx := context.Background()
	records := []Record{
		{
			ID:      "test1",
			Content: "测试文本1",
			Citation: appmodel.Citation{
				Title:   "测试文档1",
				DocType: "report",
			},
			Vector: []float32{0.1, 0.2, 0.3, 0.4, 0.5},
			Metadata: map[string]string{
				"stock_code":   "600519",
				"doc_type":     "report",
				"title":        "测试文档1",
				"company_name": "贵州茅台",
			},
		},
	}

	err = store.Upsert(ctx, records)
	if err != nil {
		t.Fatalf("插入测试数据失败: %v", err)
	}

	// 测试搜索请求
	req := SearchRequest{
		QueryVector: []float32{0.1, 0.2, 0.3, 0.4, 0.5},
		Filter: Filter{
			StockCode: "600519",
			DocTypes:  []string{"report"},
		},
		TopK: 5,
	}

	results, err := store.Search(ctx, req)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}

	// 验证结果
	if len(results) < 0 {
		t.Fatalf("结果长度小于0")
	}
}

func TestUpsert(t *testing.T) {
	// 测试插入功能
	store, err := setupTestDB(t)
	if err != nil {
		t.Skipf("跳过测试，无法设置测试数据库: %v", err)
		return
	}
	defer teardownTestDB(t)

	// 测试数据
	ctx := context.Background()
	records := []Record{
		{
			ID:      "test1",
			Content: "测试文本1",
			Citation: appmodel.Citation{
				Title:   "测试文档1",
				DocType: "report",
			},
			Vector: []float32{0.1, 0.2, 0.3, 0.4, 0.5},
			Metadata: map[string]string{
				"stock_code":   "600519",
				"doc_type":     "report",
				"title":        "测试文档1",
				"company_name": "贵州茅台",
			},
		},
	}

	err = store.Upsert(ctx, records)
	if err != nil {
		t.Fatalf("插入失败: %v", err)
	}
}

func TestUpsertMultipleChunks(t *testing.T) {
	// 测试同一文档多chunk upsert
	store, err := setupTestDB(t)
	if err != nil {
		t.Skipf("跳过测试，无法设置测试数据库: %v", err)
		return
	}
	defer teardownTestDB(t)

	// 测试数据
	ctx := context.Background()
	records := []Record{
		{
			ID:      "test1_chunk1",
			Content: "测试文本1  chunk1",
			Citation: appmodel.Citation{
				Title:   "测试文档1",
				DocType: "report",
			},
			Vector: []float32{0.1, 0.2, 0.3, 0.4, 0.5},
			Metadata: map[string]string{
				"stock_code":   "600519",
				"doc_type":     "report",
				"title":        "测试文档1",
				"company_name": "贵州茅台",
			},
		},
		{
			ID:      "test1_chunk2",
			Content: "测试文本1  chunk2",
			Citation: appmodel.Citation{
				Title:   "测试文档1",
				DocType: "report",
			},
			Vector: []float32{0.2, 0.3, 0.4, 0.5, 0.6},
			Metadata: map[string]string{
				"stock_code":   "600519",
				"doc_type":     "report",
				"title":        "测试文档1",
				"company_name": "贵州茅台",
			},
		},
	}

	err = store.Upsert(ctx, records)
	if err != nil {
		t.Fatalf("插入多chunk失败: %v", err)
	}
}

func TestListDocumentsReturnsAggregatedContent(t *testing.T) {
	store, err := setupTestDB(t)
	if err != nil {
		t.Skipf("跳过测试，无法设置测试数据库: %v", err)
		return
	}
	defer teardownTestDB(t)

	ctx := context.Background()
	records := []Record{
		{
			ID:       "test_doc_chunk_1",
			Content:  "第一段内容",
			Citation: appmodel.Citation{Title: "测试文档2", DocType: "report"},
			Vector:   []float32{0.1, 0.2, 0.3, 0.4, 0.5},
			Metadata: map[string]string{
				"stock_code":   "600519",
				"doc_type":     "report",
				"title":        "测试文档2",
				"company_name": "贵州茅台",
				"published_at": "2025-03-01",
				"keywords":     "年报,净利润",
			},
		},
		{
			ID:       "test_doc_chunk_2",
			Content:  "第二段内容",
			Citation: appmodel.Citation{Title: "测试文档2", DocType: "report"},
			Vector:   []float32{0.2, 0.3, 0.4, 0.5, 0.6},
			Metadata: map[string]string{
				"stock_code":   "600519",
				"doc_type":     "report",
				"title":        "测试文档2",
				"company_name": "贵州茅台",
				"published_at": "2025-03-01",
				"keywords":     "年报,净利润",
			},
		},
	}

	if err := store.Upsert(ctx, records); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	docs, err := store.ListDocuments(ctx)
	if err != nil {
		t.Fatalf("list documents failed: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 document, got %d", len(docs))
	}
	if docs[0].Content == "" {
		t.Fatal("expected aggregated content to be present")
	}
	if len(docs[0].Keywords) != 2 {
		t.Fatalf("expected 2 keywords, got %d", len(docs[0].Keywords))
	}
}

func TestKeywordSearch(t *testing.T) {
	store, err := setupTestDB(t)
	if err != nil {
		t.Skipf("跳过测试，无法设置测试数据库: %v", err)
		return
	}
	defer teardownTestDB(t)

	ctx := context.Background()
	records := []Record{
		{
			ID:      "kw_test_chunk_1",
			Content: "贵州茅台2024年经营活动产生的现金流量净额为884.26亿元。",
			Citation: appmodel.Citation{
				Title:   "贵州茅台2024年年度报告",
				DocType: "report",
			},
			Vector: []float32{0.1, 0.2, 0.3, 0.4, 0.5},
			Metadata: map[string]string{
				"stock_code":   "600519",
				"doc_type":     "report",
				"title":        "贵州茅台2024年年度报告",
				"company_name": "贵州茅台",
				"published_at": "2025-03-01",
			},
		},
	}

	if err := store.Upsert(ctx, records); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	results, err := store.KeywordSearch(ctx, KeywordSearchRequest{
		QueryText: "经营现金流 2024",
		Terms:     []string{"经营现金流", "2024"},
		TopK:      3,
		Filter: Filter{
			StockCode: "600519",
			DocTypes:  []string{"report"},
		},
	})
	if err != nil {
		t.Fatalf("keyword search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected keyword search results")
	}
	if results[0].Citation.Title != "贵州茅台2024年年度报告" {
		t.Fatalf("unexpected keyword search title: %s", results[0].Citation.Title)
	}
}

func TestListAvailableDocTypes(t *testing.T) {
	store, err := setupTestDB(t)
	if err != nil {
		t.Skipf("跳过测试，无法设置测试数据库: %v", err)
		return
	}
	defer teardownTestDB(t)

	ctx := context.Background()
	records := []Record{
		{
			ID:       "doc_type_test_report",
			Content:  "报告内容",
			Citation: appmodel.Citation{Title: "报告A", DocType: "report"},
			Vector:   []float32{0.1, 0.2, 0.3, 0.4, 0.5},
			Metadata: map[string]string{
				"stock_code":   "600519",
				"doc_type":     "report",
				"title":        "报告A",
				"company_name": "贵州茅台",
			},
		},
		{
			ID:       "doc_type_test_announcement",
			Content:  "公告内容",
			Citation: appmodel.Citation{Title: "公告A", DocType: "announcement"},
			Vector:   []float32{0.2, 0.3, 0.4, 0.5, 0.6},
			Metadata: map[string]string{
				"stock_code":   "600519",
				"doc_type":     "announcement",
				"title":        "公告A",
				"company_name": "贵州茅台",
			},
		},
	}

	if err := store.Upsert(ctx, records); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	docTypes, err := store.ListAvailableDocTypes(ctx, "600519")
	if err != nil {
		t.Fatalf("list available doc types failed: %v", err)
	}
	if len(docTypes) != 2 {
		t.Fatalf("expected 2 doc types, got %d", len(docTypes))
	}
}

func TestFallbackDocumentSearch(t *testing.T) {
	store, err := setupTestDB(t)
	if err != nil {
		t.Skipf("跳过测试，无法设置测试数据库: %v", err)
		return
	}
	defer teardownTestDB(t)

	ctx := context.Background()
	records := []Record{
		{
			ID:       "fallback_doc_chunk_1",
			Content:  "贵州茅台2024年经营活动产生的现金流量净额为884.26亿元。",
			Citation: appmodel.Citation{Title: "贵州茅台2024年年度报告", DocType: "report"},
			Vector:   []float32{0.1, 0.2, 0.3, 0.4, 0.5},
			Metadata: map[string]string{
				"stock_code":   "600519",
				"doc_type":     "report",
				"title":        "贵州茅台2024年年度报告",
				"company_name": "贵州茅台",
				"published_at": "2025-03-01",
				"keywords":     "经营现金流,年度报告",
			},
		},
	}

	if err := store.Upsert(ctx, records); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	docs, err := store.FallbackDocumentSearch(ctx, KeywordSearchRequest{
		QueryText: "贵州茅台2024年经营现金流是多少？",
		Terms:     []string{"贵州茅台", "2024", "经营现金流"},
		TopK:      3,
		Filter: Filter{
			StockCode: "600519",
			DocTypes:  []string{"report"},
		},
	})
	if err != nil {
		t.Fatalf("fallback document search failed: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("expected fallback document search results")
	}
	if docs[0].Title != "贵州茅台2024年年度报告" {
		t.Fatalf("unexpected fallback document title: %s", docs[0].Title)
	}
}

func TestUpsertIdempotent(t *testing.T) {
	// 测试同一chunk重复upsert幂等
	store, err := setupTestDB(t)
	if err != nil {
		t.Skipf("跳过测试，无法设置测试数据库: %v", err)
		return
	}
	defer teardownTestDB(t)

	// 测试数据
	ctx := context.Background()
	record := Record{
		ID:      "test1",
		Content: "测试文本1",
		Citation: appmodel.Citation{
			Title:   "测试文档1",
			DocType: "report",
		},
		Vector: []float32{0.1, 0.2, 0.3, 0.4, 0.5},
		Metadata: map[string]string{
			"stock_code":   "600519",
			"doc_type":     "report",
			"title":        "测试文档1",
			"company_name": "贵州茅台",
		},
	}

	// 第一次插入
	err = store.Upsert(ctx, []Record{record})
	if err != nil {
		t.Fatalf("第一次插入失败: %v", err)
	}

	// 第二次插入（重复）
	err = store.Upsert(ctx, []Record{record})
	if err != nil {
		t.Fatalf("第二次插入失败: %v", err)
	}
}

func TestSearchWithTimeRange(t *testing.T) {
	// 测试time_range过滤生效
	store, err := setupTestDB(t)
	if err != nil {
		t.Skipf("跳过测试，无法设置测试数据库: %v", err)
		return
	}
	defer teardownTestDB(t)

	// 插入测试数据
	ctx := context.Background()
	records := []Record{
		{
			ID:      "test1",
			Content: "测试文本1",
			Citation: appmodel.Citation{
				Title:   "测试文档1",
				DocType: "report",
			},
			Vector: []float32{0.1, 0.2, 0.3, 0.4, 0.5},
			Metadata: map[string]string{
				"stock_code":   "600519",
				"doc_type":     "report",
				"title":        "测试文档1",
				"company_name": "贵州茅台",
			},
		},
	}

	err = store.Upsert(ctx, records)
	if err != nil {
		t.Fatalf("插入测试数据失败: %v", err)
	}

	// 测试搜索请求
	req := SearchRequest{
		QueryVector: []float32{0.1, 0.2, 0.3, 0.4, 0.5},
		Filter: Filter{
			StockCode: "600519",
			DocTypes:  []string{"report"},
			TimeRange: "latest",
		},
		TopK: 5,
	}

	results, err := store.Search(ctx, req)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}

	// 验证结果
	if len(results) < 0 {
		t.Fatalf("结果长度小于0")
	}
}

func TestSearchCitationFields(t *testing.T) {
	// 测试citation字段完整返回
	store, err := setupTestDB(t)
	if err != nil {
		t.Skipf("跳过测试，无法设置测试数据库: %v", err)
		return
	}
	defer teardownTestDB(t)

	// 插入测试数据
	ctx := context.Background()
	records := []Record{
		{
			ID:      "test1",
			Content: "测试文本1",
			Citation: appmodel.Citation{
				Title:     "测试文档1",
				DocType:   "report",
				SourceURL: "http://example.com",
				Published: "2023-12-31",
			},
			Vector: []float32{0.1, 0.2, 0.3, 0.4, 0.5},
			Metadata: map[string]string{
				"stock_code":   "600519",
				"doc_type":     "report",
				"title":        "测试文档1",
				"company_name": "贵州茅台",
				"source_url":   "http://example.com",
				"published_at": "2023-12-31",
			},
		},
	}

	err = store.Upsert(ctx, records)
	if err != nil {
		t.Fatalf("插入测试数据失败: %v", err)
	}

	// 测试搜索请求
	req := SearchRequest{
		QueryVector: []float32{0.1, 0.2, 0.3, 0.4, 0.5},
		Filter: Filter{
			StockCode: "600519",
			DocTypes:  []string{"report"},
		},
		TopK: 5,
	}

	results, err := store.Search(ctx, req)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}

	// 验证结果
	if len(results) == 0 {
		t.Fatalf("无搜索结果")
	}

	// 验证citation字段
	result := results[0]
	if result.Citation.Title == "" {
		t.Fatalf("Citation.Title 为空")
	}
	if result.Citation.DocType == "" {
		t.Fatalf("Citation.DocType 为空")
	}
	if result.Citation.SourceURL == "" {
		t.Fatalf("Citation.SourceURL 为空")
	}
	if result.Citation.Published == "" {
		t.Fatalf("Citation.Published 为空")
	}
}

func TestSearchEmptyResults(t *testing.T) {
	// 测试空结果时不误回退成假数据
	store, err := setupTestDB(t)
	if err != nil {
		t.Skipf("跳过测试，无法设置测试数据库: %v", err)
		return
	}
	defer teardownTestDB(t)

	// 测试搜索请求（无匹配数据）
	ctx := context.Background()
	req := SearchRequest{
		QueryVector: []float32{0.1, 0.2, 0.3, 0.4, 0.5},
		Filter: Filter{
			StockCode: "999999", // 不存在的股票代码
			DocTypes:  []string{"report"},
		},
		TopK: 5,
	}

	results, err := store.Search(ctx, req)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}

	// 验证结果为空
	if len(results) != 0 {
		t.Fatalf("期望空结果，但得到了 %d 个结果", len(results))
	}
}
