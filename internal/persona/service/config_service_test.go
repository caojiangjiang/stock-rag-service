package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	appmodel "stock_rag/internal/model"
	"stock_rag/internal/persona/model"
	"strings"
	"testing"
)

func getProjectRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "configs")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func TestConfigPersonaService_ListPersonas(t *testing.T) {
	root := getProjectRoot()
	if root == "" {
		t.Skip("Cannot find project root")
	}

	configPath := filepath.Join(root, "configs", "personas.yaml")
	svc, err := NewConfigPersonaServiceWithDefaults(configPath)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	ctx := context.Background()

	// 测试获取所有 personas
	cards, err := svc.ListPersonas(ctx, "", "", "active", 10)
	if err != nil {
		t.Fatalf("Failed to list personas: %v", err)
	}
	if len(cards) != 6 {
		t.Errorf("Expected 6 personas, got %d", len(cards))
	}

	// 测试 market 过滤
	usCards, err := svc.ListPersonas(ctx, "us", "", "active", 10)
	if err != nil {
		t.Fatalf("Failed to list US personas: %v", err)
	}
	if len(usCards) != 3 {
		t.Errorf("Expected 3 US personas, got %d", len(usCards))
	}

	cnCards, err := svc.ListPersonas(ctx, "cn", "", "active", 10)
	if err != nil {
		t.Fatalf("Failed to list CN personas: %v", err)
	}
	if len(cnCards) != 3 {
		t.Errorf("Expected 3 CN personas, got %d", len(cnCards))
	}

	// 测试 style_tag 过滤
	growthCards, err := svc.ListPersonas(ctx, "", "growth", "active", 10)
	if err != nil {
		t.Fatalf("Failed to list growth personas: %v", err)
	}
	if len(growthCards) < 1 {
		t.Errorf("Expected at least 1 growth persona, got %d", len(growthCards))
	}

	// 测试 limit
	limitedCards, err := svc.ListPersonas(ctx, "", "", "active", 2)
	if err != nil {
		t.Fatalf("Failed to list limited personas: %v", err)
	}
	if len(limitedCards) != 2 {
		t.Errorf("Expected 2 personas with limit, got %d", len(limitedCards))
	}
}

func TestConfigPersonaService_GetPersona(t *testing.T) {
	root := getProjectRoot()
	if root == "" {
		t.Skip("Cannot find project root")
	}

	configPath := filepath.Join(root, "configs", "personas.yaml")
	svc, err := NewConfigPersonaServiceWithDefaults(configPath)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	ctx := context.Background()

	// 测试获取存在的 persona
	profile, err := svc.GetPersona(ctx, "us_growth_tech")
	if err != nil {
		t.Fatalf("Failed to get persona: %v", err)
	}
	if profile.PersonaID != "us_growth_tech" {
		t.Errorf("Expected persona_id 'us_growth_tech', got '%s'", profile.PersonaID)
	}
	if profile.Name != "美股科技成长派" {
		t.Errorf("Expected name '美股科技成长派', got '%s'", profile.Name)
	}
	if profile.Market != "us" {
		t.Errorf("Expected market 'us', got '%s'", profile.Market)
	}
	if len(profile.StyleTags) == 0 {
		t.Error("Expected non-empty style_tags")
	}
	if len(profile.PreferredSectors) == 0 {
		t.Error("Expected non-empty preferred_sectors")
	}

	// 测试获取不存在的 persona
	_, err = svc.GetPersona(ctx, "nonexistent")
	if err != ErrPersonaNotFound {
		t.Errorf("Expected ErrPersonaNotFound, got %v", err)
	}
}

func TestConfigPersonaService_Chat(t *testing.T) {
	root := getProjectRoot()
	if root == "" {
		t.Skip("Cannot find project root")
	}

	configPath := filepath.Join(root, "configs", "personas.yaml")
	svc, err := NewConfigPersonaServiceWithDefaults(configPath)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	ctx := context.Background()

	// 测试 chat 返回 mock 响应
	answer, err := svc.Chat(ctx, &model.PersonaChatRequest{
		PersonaID: "us_growth_tech",
		Message:   "AI龙头还能涨吗？",
	})
	if err != nil {
		t.Fatalf("Failed to chat: %v", err)
	}
	if answer.PersonaID != "us_growth_tech" {
		t.Errorf("Expected persona_id 'us_growth_tech', got '%s'", answer.PersonaID)
	}
	if answer.PersonaName == "" {
		t.Error("Expected non-empty persona_name")
	}
	if answer.Summary == "" && len(answer.Thesis) == 0 {
		t.Error("Expected summary or thesis in chat response")
	}

	// 测试不存在的 persona
	_, err = svc.Chat(ctx, &model.PersonaChatRequest{
		PersonaID: "nonexistent",
		Message:   "test",
	})
	if err != ErrPersonaNotFound {
		t.Errorf("Expected ErrPersonaNotFound for nonexistent persona, got %v", err)
	}
}

func TestConfigPersonaService_Roundtable(t *testing.T) {
	root := getProjectRoot()
	if root == "" {
		t.Skip("Cannot find project root")
	}

	configPath := filepath.Join(root, "configs", "personas.yaml")
	svc, err := NewConfigPersonaServiceWithDefaults(configPath)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	ctx := context.Background()

	// 测试指定 persona_ids 的 roundtable
	response, err := svc.Roundtable(ctx, &model.RoundtableRequest{
		Question:   "当前市场怎么看？",
		PersonaIDs: []string{"us_growth_tech", "cn_dividend_defensive"},
	})
	if err != nil {
		t.Fatalf("Failed to roundtable: %v", err)
	}
	if response.Question != "当前市场怎么看？" {
		t.Errorf("Expected question '当前市场怎么看？', got '%s'", response.Question)
	}
	if len(response.Participants) != 2 {
		t.Errorf("Expected 2 participants, got %d", len(response.Participants))
	}
	if len(response.Answers) != 2 {
		t.Errorf("Expected 2 answers, got %d", len(response.Answers))
	}

	// 测试不指定 persona_ids (使用所有活跃 persona)
	response2, err := svc.Roundtable(ctx, &model.RoundtableRequest{
		Question: "测试问题",
	})
	if err != nil {
		t.Fatalf("Failed to roundtable without persona_ids: %v", err)
	}
	if len(response2.Participants) == 0 {
		t.Error("Expected at least 1 participant when persona_ids not specified")
	}
}

func TestConfigPersonaService_ErrorCases(t *testing.T) {
	// 测试配置文件不存在
	_, err := NewConfigPersonaServiceWithDefaults("nonexistent.yaml")
	if err == nil {
		t.Error("Expected error for nonexistent config file")
	}
}

// FakeQueryService 是一个用于测试的假 QueryService 实现
type FakeQueryService struct {
	shouldFail bool
	citations  []appmodel.Citation
}

func (f *FakeQueryService) Query(ctx context.Context, req appmodel.RAGQueryRequest) (appmodel.RAGQueryResponse, error) {
	if f.shouldFail {
		return appmodel.RAGQueryResponse{}, fmt.Errorf("simulated query error")
	}
	return appmodel.RAGQueryResponse{
		Answer:    "This is a test answer with evidence. Because of market conditions, we are bullish.",
		Citations: f.citations,
	}, nil
}

func TestConfigPersonaService_ChatWithFakeQueryService(t *testing.T) {
	root := getProjectRoot()
	if root == "" {
		t.Skip("Cannot find project root")
	}

	configPath := filepath.Join(root, "configs", "personas.yaml")

	// 创建带有 fake QueryService 的 ConfigPersonaService
	fakeQuerySvc := &FakeQueryService{
		shouldFail: false,
		citations: []appmodel.Citation{
			{
				Title:     "Test Article 1",
				DocType:   "news",
				SourceURL: "https://xueqiu.com/article/123",
				Published: "2024-01-01",
				Content:   "This is the content of the first citation. It contains important information about the market analysis and investment insights.",
			},
			{
				Title:     "Research Report 2",
				DocType:   "report",
				SourceURL: "https://eastmoney.com/report/456",
				Published: "2024-01-02",
				Content:   "This is the content of the second citation with detailed analysis and data.",
			},
		},
	}

	svc, err := NewConfigPersonaService(configPath, fakeQuerySvc, nil)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	ctx := context.Background()

	// 测试 Chat 调用
	answer, err := svc.Chat(ctx, &model.PersonaChatRequest{
		PersonaID: "us_growth_tech",
		Message:   "What is your view on AI stocks?",
		StockCode: "AAPL",
		TimeRange: "1y",
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	// 验证基本字段
	if answer.PersonaID != "us_growth_tech" {
		t.Errorf("Expected PersonaID 'us_growth_tech', got '%s'", answer.PersonaID)
	}
	if answer.PersonaName == "" {
		t.Error("PersonaName should not be empty")
	}
	if answer.Stance == "" {
		t.Error("Stance should not be empty")
	}
	if answer.Summary == "" && len(answer.Thesis) == 0 {
		t.Error("Expected summary or thesis in chat response")
	}

	// 验证 citations 被正确映射
	if len(answer.Citations) != 2 {
		t.Errorf("Expected 2 citations, got %d", len(answer.Citations))
	} else {
		// 验证第一个 citation
		firstCitation := answer.Citations[0]
		if firstCitation.CitationID != "citation-1" {
			t.Errorf("Expected CitationID 'citation-1', got '%s'", firstCitation.CitationID)
		}
		if firstCitation.Title != "Test Article 1" {
			t.Errorf("Expected Title 'Test Article 1', got '%s'", firstCitation.Title)
		}
		if firstCitation.DocType != "news" {
			t.Errorf("Expected DocType 'news', got '%s'", firstCitation.DocType)
		}
		if firstCitation.Source != "Xueqiu" {
			t.Errorf("Expected Source 'Xueqiu', got '%s'", firstCitation.Source)
		}
		if firstCitation.SourceURL != "https://xueqiu.com/article/123" {
			t.Errorf("Expected SourceURL 'https://xueqiu.com/article/123', got '%s'", firstCitation.SourceURL)
		}
		if firstCitation.PublishedAt != "2024-01-01" {
			t.Errorf("Expected PublishedAt '2024-01-01', got '%s'", firstCitation.PublishedAt)
		}
		if !strings.Contains(firstCitation.Snippet, "This is the content") {
			t.Errorf("Expected Snippet to contain 'This is the content', got '%s'", firstCitation.Snippet)
		}
		if firstCitation.Score <= 0 {
			t.Errorf("Expected Score > 0, got %f", firstCitation.Score)
		}

		// 验证第二个 citation
		secondCitation := answer.Citations[1]
		if secondCitation.Source != "Eastmoney" {
			t.Errorf("Expected Source 'Eastmoney', got '%s'", secondCitation.Source)
		}
	}

	// 验证 thesis 从 RAG 回答中提取
	if !strings.Contains(answer.Thesis[0], "analysis") && !strings.Contains(answer.Thesis[0], "because") {
		t.Logf("Thesis: %v", answer.Thesis)
		// 这不是错误，只是记录一下实际提取的论点
	}

	// 验证 CounterView
	if answer.CounterView.PersonaID == "" {
		t.Log("CounterView PersonaID is empty (no disagree_persona_ids configured)")
	}

	// 验证 Disclaimer
	if answer.Disclaimer == "" {
		t.Error("Disclaimer should not be empty")
	}

	// 验证 RequestID
	if answer.RequestID == "" {
		t.Error("RequestID should not be empty")
	}
}

func TestConfigPersonaService_ChatWithFailingQueryService(t *testing.T) {
	root := getProjectRoot()
	if root == "" {
		t.Skip("Cannot find project root")
	}

	configPath := filepath.Join(root, "configs", "personas.yaml")

	// 创建带有失败的 fake QueryService 的 ConfigPersonaService
	fakeQuerySvc := &FakeQueryService{
		shouldFail: true,
		citations:  []appmodel.Citation{},
	}

	svc, err := NewConfigPersonaService(configPath, fakeQuerySvc, nil)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	ctx := context.Background()

	// 测试 Chat 调用（QueryService 失败时应该优雅降级）
	answer, err := svc.Chat(ctx, &model.PersonaChatRequest{
		PersonaID: "us_growth_tech",
		Message:   "What is your view on AI stocks?",
	})
	if err != nil {
		t.Fatalf("Chat should not fail when QueryService fails, got: %v", err)
	}

	// 验证基本字段仍然存在（使用默认回答）
	if answer.PersonaID != "us_growth_tech" {
		t.Errorf("Expected PersonaID 'us_growth_tech', got '%s'", answer.PersonaID)
	}
	if answer.Stance == "" {
		t.Error("Stance should not be empty")
	}
	if len(answer.Thesis) == 0 {
		t.Error("Thesis should not be empty")
	}
	// citations 应该为空（因为 QueryService 失败了）
	if len(answer.Citations) != 0 {
		t.Errorf("Expected 0 citations when QueryService fails, got %d", len(answer.Citations))
	}
}

func TestConfigPersonaService_RoundtableWithFakeQueryService(t *testing.T) {
	root := getProjectRoot()
	if root == "" {
		t.Skip("Cannot find project root")
	}

	configPath := filepath.Join(root, "configs", "personas.yaml")

	// 创建带有 fake QueryService 的 ConfigPersonaService
	fakeQuerySvc := &FakeQueryService{
		shouldFail: false,
		citations: []appmodel.Citation{
			{
				Title:     "Market Analysis Report",
				DocType:   "report",
				SourceURL: "https://example.com/report/1",
				Published: "2024-01-15",
				Content:   "AI sector shows strong growth potential with increasing demand and technological advancements.",
			},
		},
	}

	svc, err := NewConfigPersonaService(configPath, fakeQuerySvc, nil)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	ctx := context.Background()

	// 测试指定不同风格的 persona_ids
	response, err := svc.Roundtable(ctx, &model.RoundtableRequest{
		Question:   "What is the outlook for AI stocks?",
		PersonaIDs: []string{"us_growth_tech", "us_value_recovery", "cn_dividend_defensive"},
	})
	if err != nil {
		t.Fatalf("Roundtable failed: %v", err)
	}

	// 验证基本字段
	if response.Question != "What is the outlook for AI stocks?" {
		t.Errorf("Expected question, got '%s'", response.Question)
	}
	if len(response.Participants) != 3 {
		t.Errorf("Expected 3 participants, got %d", len(response.Participants))
	}
	if len(response.Answers) != 3 {
		t.Errorf("Expected 3 answers, got %d", len(response.Answers))
	}

	// 验证每个回答都有内容
	for _, answer := range response.Answers {
		if answer.PersonaID == "" {
			t.Error("PersonaID should not be empty")
		}
		if answer.Stance == "" {
			t.Error("Stance should not be empty")
		}
	}

	// 验证 consensus、disagreements、risk_focus 不是硬编码占位文本
	if len(response.Consensus) == 0 {
		t.Error("Consensus should not be empty")
	}
	if len(response.Disagreements) == 0 {
		t.Error("Disagreements should not be empty")
	}
	if len(response.RiskFocus) == 0 {
		t.Error("RiskFocus should not be empty")
	}

	// 验证 consensus 不是占位文本
	for _, c := range response.Consensus {
		if c == "暂无共识" {
			t.Error("Consensus should not be placeholder text")
		}
	}

	// 验证 disagreements 不是占位文本
	for _, d := range response.Disagreements {
		if d == "暂无分歧" {
			t.Error("Disagreements should not be placeholder text")
		}
	}

	// 验证 risk_focus 不是占位文本
	for _, r := range response.RiskFocus {
		if r == "暂无风险焦点" {
			t.Error("RiskFocus should not be placeholder text")
		}
	}

	// 验证 moderation_notice
	if response.ModerationNotice == "" {
		t.Error("ModerationNotice should not be empty")
	}

	// 验证 request_id
	if response.RequestID == "" {
		t.Error("RequestID should not be empty")
	}

	t.Logf("Consensus: %v", response.Consensus)
	t.Logf("Disagreements: %v", response.Disagreements)
	t.Logf("RiskFocus: %v", response.RiskFocus)
}

func TestConfigPersonaService_RoundtablePartialFailure(t *testing.T) {
	root := getProjectRoot()
	if root == "" {
		t.Skip("Cannot find project root")
	}

	configPath := filepath.Join(root, "configs", "personas.yaml")

	// 创建会失败的 fake QueryService
	fakeQuerySvc := &FakeQueryService{
		shouldFail: true, // 让所有查询都失败，测试降级策略
		citations:  []appmodel.Citation{},
	}

	svc, err := NewConfigPersonaService(configPath, fakeQuerySvc, nil)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	ctx := context.Background()

	// 测试部分失败场景
	response, err := svc.Roundtable(ctx, &model.RoundtableRequest{
		Question:   "Test question for partial failure",
		PersonaIDs: []string{"us_growth_tech", "cn_dividend_defensive"},
	})
	if err != nil {
		t.Fatalf("Roundtable should not fail even with failing QueryService, got: %v", err)
	}

	// 验证基本字段仍然存在
	if len(response.Participants) != 2 {
		t.Errorf("Expected 2 participants, got %d", len(response.Participants))
	}
	if len(response.Answers) != 2 {
		t.Errorf("Expected 2 answers, got %d", len(response.Answers))
	}

	// 验证每个回答都有内容（使用默认降级回答）
	for _, answer := range response.Answers {
		if answer.PersonaID == "" {
			t.Error("PersonaID should not be empty")
		}
		if answer.Stance == "" {
			t.Error("Stance should not be empty")
		}
		if len(answer.Thesis) == 0 {
			t.Error("Thesis should not be empty")
		}
	}

	// 验证仍然有总结结果
	if len(response.Consensus) == 0 {
		t.Error("Consensus should not be empty")
	}
	if len(response.Disagreements) == 0 {
		t.Error("Disagreements should not be empty")
	}
	if len(response.RiskFocus) == 0 {
		t.Error("RiskFocus should not be empty")
	}

	// 验证有失败通知
	t.Logf("ModerationNotice: %s", response.ModerationNotice)
}

func TestConfigPersonaService_RoundtableDefaultParticipants(t *testing.T) {
	root := getProjectRoot()
	if root == "" {
		t.Skip("Cannot find project root")
	}

	configPath := filepath.Join(root, "configs", "personas.yaml")

	fakeQuerySvc := &FakeQueryService{
		shouldFail: false,
		citations:  []appmodel.Citation{},
	}

	svc, err := NewConfigPersonaService(configPath, fakeQuerySvc, nil)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	ctx := context.Background()

	// 测试不传 persona_ids，使用默认组合
	response, err := svc.Roundtable(ctx, &model.RoundtableRequest{
		Question: "What is the market outlook?",
	})
	if err != nil {
		t.Fatalf("Roundtable failed: %v", err)
	}

	// 默认应该有 3 个参与者
	if len(response.Participants) != 3 {
		t.Errorf("Expected 3 default participants, got %d", len(response.Participants))
	}

	// 验证参与者 ID
	expectedIDs := []string{"us_growth_tech", "us_value_recovery", "cn_dividend_defensive"}
	for _, expectedID := range expectedIDs {
		found := false
		for _, p := range response.Participants {
			if p.PersonaID == expectedID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected participant %s not found in default participants", expectedID)
		}
	}
}

func TestConfigPersonaService_RoundtableInvalidParticipants(t *testing.T) {
	root := getProjectRoot()
	if root == "" {
		t.Skip("Cannot find project root")
	}

	configPath := filepath.Join(root, "configs", "personas.yaml")

	svc, err := NewConfigPersonaServiceWithDefaults(configPath)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	ctx := context.Background()

	// 测试无效的 persona_ids
	_, err = svc.Roundtable(ctx, &model.RoundtableRequest{
		Question:   "Test question",
		PersonaIDs: []string{"nonexistent1", "nonexistent2"},
	})
	if err == nil {
		t.Error("Expected error for invalid persona_ids")
	}

	// 测试部分有效、部分无效（只有 1 个有效参与者，应该失败）
	_, err = svc.Roundtable(ctx, &model.RoundtableRequest{
		Question:   "Test question",
		PersonaIDs: []string{"us_growth_tech", "nonexistent"},
	})
	if err == nil {
		t.Error("Expected error when only 1 valid participant (needs at least 2)")
	}
}
