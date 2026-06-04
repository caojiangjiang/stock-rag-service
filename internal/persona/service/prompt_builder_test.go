package service

import (
	"testing"

	"stock_rag/internal/persona/model"
)

func TestPersonaPromptBuilder_BuildQueryPrompt(t *testing.T) {
	profile := &model.PersonaProfile{
		PersonaID:        "test_persona",
		Name:             "Growth Investor",
		Market:           "us",
		StyleTags:        []string{"growth", "tech"},
		InvestmentBelief: "Invest in companies with strong growth potential",
		PreferredSectors: []string{"Technology", "Biotech"},
		DecisionFramework: []string{
			"Focus on revenue growth",
			"Check market share",
		},
	}

	builder := NewPersonaPromptBuilder(profile)
	prompt := builder.BuildQueryPrompt("What is your view on AI stocks?")

	if !contains(prompt, "Growth Investor") {
		t.Error("Prompt should contain persona name")
	}
	if !contains(prompt, "growth") {
		t.Error("Prompt should contain style tags")
	}
	if !contains(prompt, "Technology") {
		t.Error("Prompt should contain preferred sectors")
	}
	if !contains(prompt, "请使用中文") {
		t.Error("Prompt should request Chinese answer")
	}
	if !contains(prompt, "What is your view on AI stocks?") {
		t.Error("Prompt should contain the question")
	}
}

func TestPersonaPromptBuilder_BuildStance(t *testing.T) {
	tests := []struct {
		name           string
		profile        *model.PersonaProfile
		expectedStance string
	}{
		{
			name: "growth style persona",
			profile: &model.PersonaProfile{
				Name:      "Growth Investor",
				StyleTags: []string{"growth", "tech"},
				Performance: model.PersonaPerformanceSummary{
					MaxDrawdown1Y: -15.0,
				},
			},
			expectedStance: "美股科技成长派",
		},
		{
			name: "value style persona",
			profile: &model.PersonaProfile{
				Name:      "Value Investor",
				StyleTags: []string{"value", "dividend"},
				Performance: model.PersonaPerformanceSummary{
					MaxDrawdown1Y: -8.0,
				},
			},
			expectedStance: "Value Investor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPersonaPromptBuilder(tt.profile)
			stance := builder.BuildStance("test question", "")
			if !contains(stance, tt.expectedStance) {
				t.Errorf("Expected stance to contain %q, got %q", tt.expectedStance, stance)
			}
		})
	}
}

func TestPersonaPromptBuilder_BuildThesis(t *testing.T) {
	profile := &model.PersonaProfile{
		Name:             "Test Investor",
		InvestmentBelief: "Test belief",
		DecisionFramework: []string{
			"Framework 1",
			"Framework 2",
			"Framework 3",
		},
	}

	builder := NewPersonaPromptBuilder(profile)

	// 测试从空回答中提取论点（应使用默认框架）
	thesis := builder.BuildThesis("")
	if len(thesis) == 0 {
		t.Error("Thesis should not be empty")
	}

	// 测试从回答中提取论点
	ragAnswer := "This is a test. The analysis shows strong fundamentals. Because of market trends, we are bullish."
	thesis = builder.BuildThesis(ragAnswer)
	if len(thesis) == 0 {
		t.Error("Thesis should not be empty")
	}
}

func TestPersonaPromptBuilder_BuildRisks(t *testing.T) {
	// 测试使用配置的风险
	profileWithRisks := &model.PersonaProfile{
		Name:         "Test Investor",
		TypicalRisks: []string{"Risk 1", "Risk 2"},
	}

	builder := NewPersonaPromptBuilder(profileWithRisks)
	risks := builder.BuildRisks()
	if len(risks) != 2 {
		t.Errorf("Expected 2 risks, got %d", len(risks))
	}

	// 测试使用默认风险
	profileWithoutRisks := &model.PersonaProfile{
		Name: "Test Investor",
	}

	builder = NewPersonaPromptBuilder(profileWithoutRisks)
	risks = builder.BuildRisks()
	if len(risks) == 0 {
		t.Error("Expected default risks")
	}
}

func TestMapCitationToEvidence(t *testing.T) {
	// 测试 extractSource 函数
	source := extractSource("https://xueqiu.com/article/123")
	if source != "雪球" {
		t.Errorf("Expected '雪球', got %q", source)
	}

	truncated := truncateContent("Short content", 200)
	if truncated != "Short content" {
		t.Errorf("Expected 'Short content', got %q", truncated)
	}

	longContent := "A" + string(make([]byte, 250))
	truncated = truncateContent(longContent, 200)
	if len(truncated) != 203 { // 200 chars + "..."
		t.Errorf("Expected length 203, got %d", len(truncated))
	}
}

func TestBuildCounterViewSummary(t *testing.T) {
	tests := []struct {
		name     string
		profile  *model.PersonaProfile
		expected string
	}{
		{
			name: "growth persona has value counter view",
			profile: &model.PersonaProfile{
				Name:               "Growth Investor",
				StyleTags:          []string{"growth"},
				DisagreePersonaIDs: []string{"value_persona"},
			},
			expected: "价值派投资者",
		},
		{
			name: "value persona has growth counter view",
			profile: &model.PersonaProfile{
				Name:               "Value Investor",
				StyleTags:          []string{"value"},
				DisagreePersonaIDs: []string{"growth_persona"},
			},
			expected: "成长派投资者",
		},
		{
			name: "persona without disagree IDs",
			profile: &model.PersonaProfile{
				Name:               "Neutral Investor",
				DisagreePersonaIDs: []string{},
			},
			expected: "相反角度",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := buildCounterViewSummary(tt.profile, "test question")
			if !contains(summary, tt.expected) {
				t.Errorf("Expected summary to contain %q, got %q", tt.expected, summary)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
