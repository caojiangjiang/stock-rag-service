package chain

import (
	"context"
	"strings"
	"testing"

	einomodel "stock_rag/internal/eino/model"
	ragretriever "stock_rag/internal/eino/retriever"
	appmodel "stock_rag/internal/model"
)

// mockRetriever 是一个模拟的检索器，总是返回一些 chunks
type mockRetriever struct{}

func (r mockRetriever) Retrieve(ctx context.Context, req appmodel.RAGQueryRequest) ([]ragretriever.RetrievedChunk, error) {
	return []ragretriever.RetrievedChunk{
		{
			Content: "这是一个模拟的检索结果，包含了一些测试数据。",
			Citation: appmodel.Citation{
				Title:        "测试文档",
				DocType:      "report",
				SourceURL:    "http://example.com/test",
				Published:    "2026-01-01",
				PageNo:       1,
				SectionTitle: "测试章节",
			},
		},
	}, nil
}

func TestNewSkeletonRunnerInvoke(t *testing.T) {
	t.Setenv("ARK_API_KEY", "")
	t.Setenv("ARK_MODEL", "")

	chatModel, err := einomodel.NewChatModel(context.Background(), einomodel.DefaultChatConfigWithDefaults())
	if err != nil {
		t.Fatalf("new chat model: %v", err)
	}

	// 使用 mock retriever 替代 LocalSampleRetriever
	runner, err := NewSkeletonRunner(context.Background(), chatModel, mockRetriever{})
	if err != nil {
		t.Fatalf("compile runner: %v", err)
	}

	resp, err := runner.Invoke(context.Background(), appmodel.RAGQueryRequest{
		Question:  "总结近期公告重点",
		StockCode: "600519",
		TimeRange: "30d",
	})
	if err != nil {
		t.Fatalf("invoke runner: %v", err)
	}

	if resp.Answer == "" {
		t.Fatal("expected non-empty answer")
	}

	if !strings.Contains(resp.Answer, "Eino skeleton") {
		t.Fatalf("expected answer to mention skeleton chain, got: %s", resp.Answer)
	}

	if resp.RetrievedCount == 0 {
		t.Fatal("expected retrieved chunks")
	}

	if len(resp.Citations) == 0 {
		t.Fatal("expected citations")
	}

	if resp.RequestID == "" {
		t.Fatal("expected request id")
	}
}

func TestBuildGuardedAnswerBlocksMismatchedFiscalYearMetric(t *testing.T) {
	guarded, ok := BuildGuardedAnswer(appmodel.RAGQueryRequest{
		Question:  "贵州茅台2025年盈利怎么样？",
		StockCode: "600519",
	}, []ragretriever.RetrievedChunk{{
		Content: "贵州茅台发布2024年年度报告，净利润875.54亿元。",
		Citation: appmodel.Citation{
			Title:     "贵州茅台2024年业绩超预期",
			DocType:   "news",
			Published: "2025-03-29",
		},
	}})
	if !ok {
		t.Fatal("expected answer guard to trigger")
	}
	if !strings.Contains(guarded, "2025年净利润") {
		t.Fatalf("expected guarded answer to mention 2025年净利润, got %s", guarded)
	}
}

func TestApplyAnswerGuardBlocksAnswerYearMismatch(t *testing.T) {
	answer := ApplyAnswerGuard(appmodel.RAGQueryRequest{
		Question:  "贵州茅台2025年盈利怎么样？",
		StockCode: "600519",
	}, []ragretriever.RetrievedChunk{{
		Content: "贵州茅台2025年净利润预计保持增长。",
		Citation: appmodel.Citation{
			Title:     "贵州茅台2025年业绩预告",
			DocType:   "announcement",
			Published: "2026-01-15",
		},
	}}, "根据资料，贵州茅台2024年净利润为875.54亿元。")
	if !strings.Contains(answer, "2025年净利润") {
		t.Fatalf("expected guarded answer after year mismatch, got %s", answer)
	}
}

func TestApplyAnswerGuardRecoversMetricValueFromEvidence(t *testing.T) {
	answer := ApplyAnswerGuard(appmodel.RAGQueryRequest{
		Question:  "贵州茅台2024年经营活动产生的现金流量净额是多少？",
		StockCode: "600519",
	}, []ragretriever.RetrievedChunk{{
		Content: "第三节 管理层讨论与分析\n（三）主要财务指标\n- 基本每股收益67.84元\n- 净资产收益率32.54%\n- 经营活动产生的现金流量净额884.26亿元",
		Citation: appmodel.Citation{
			Title:     "贵州茅台酒股份有限公司2024年年度报告",
			DocType:   "report",
			Published: "2025-03-28",
		},
	}}, "现有检索资料未明确披露贵州茅台2024年经营活动产生的现金流量净额相关信息，证据不足，无法回答该问题。")

	if !strings.Contains(answer, "经营活动产生的现金流量净额") {
		t.Fatalf("expected recovered answer to mention metric, got %s", answer)
	}
	if !strings.Contains(answer, "884.26亿元") {
		t.Fatalf("expected recovered answer to mention value, got %s", answer)
	}
	if !strings.Contains(answer, "贵州茅台酒股份有限公司2024年年度报告") {
		t.Fatalf("expected recovered answer to mention citation title, got %s", answer)
	}
}

func TestApplyAnswerGuardRecoversEPSValueFromEvidence(t *testing.T) {
	answer := ApplyAnswerGuard(appmodel.RAGQueryRequest{
		Question:  "贵州茅台2024年每股收益是多少？",
		StockCode: "600519",
	}, []ragretriever.RetrievedChunk{{
		Content: "第三节 管理层讨论与分析\n（三）主要财务指标\n- 基本每股收益67.84元\n- 净资产收益率32.54%",
		Citation: appmodel.Citation{
			Title:     "贵州茅台酒股份有限公司2024年年度报告",
			DocType:   "report",
			Published: "2025-03-28",
		},
	}}, "现有检索资料未明确披露所提及的财务指标对应的报告期为2024年，无法确认贵州茅台2024年每股收益的准确数据。")

	if !strings.Contains(answer, "每股收益") {
		t.Fatalf("expected recovered answer to mention EPS metric, got %s", answer)
	}
	if !strings.Contains(answer, "67.84元") {
		t.Fatalf("expected recovered answer to mention EPS value, got %s", answer)
	}
}

func TestApplyAnswerGuardPolishesBorderlineSummaryAnswer(t *testing.T) {
	answer := ApplyAnswerGuard(appmodel.RAGQueryRequest{
		Question:  "贵州茅台最近渠道和现金流表现怎么样？",
		StockCode: "600519",
	}, []ragretriever.RetrievedChunk{{
		Content: "季报摘要显示，贵州茅台高端产品收入保持稳健增长，渠道与现金流表现受到投资者关注。",
		Citation: appmodel.Citation{
			Title:     "贵州茅台季报摘要：高端产品收入稳健",
			DocType:   "report",
			Published: "2026-03-01",
		},
	}}, "现有检索资料仅披露：对应90天期内贵州茅台高端产品收入保持稳健增长，其渠道与现金流表现受到投资者关注[1]，未披露渠道运营情况、现金流相关指标的具体表现数据，无法回答相关问题。\n引用来源：[1] 贵州茅台近90天内相关季报摘要")

	if strings.Contains(answer, "证据不足") || strings.Contains(answer, "无法回答") || strings.Contains(answer, "未披露") {
		t.Fatalf("expected polished answer without refusal wording, got %s", answer)
	}
	if !strings.Contains(answer, "高端产品收入保持稳健增长") {
		t.Fatalf("expected polished answer to preserve factual summary, got %s", answer)
	}
	if !strings.Contains(answer, "贵州茅台季报摘要：高端产品收入稳健") {
		t.Fatalf("expected polished answer to contain citation title, got %s", answer)
	}
}
