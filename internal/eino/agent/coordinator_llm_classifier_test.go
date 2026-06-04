package agent

import (
	"testing"

	"stock_rag/internal/router"
)

func TestCoordinatorLLMClassifier_ParseResponse(t *testing.T) {
	// 测试 JSON 解析
	response := `{
		"type": "plan",
		"confidence": 0.85,
		"reason": "需要多步骤规划",
		"candidates": [
			{"type": "plan", "confidence": 0.85},
			{"type": "supervisor", "confidence": 0.15}
		]
	}`

	result, _ := parseCoordinatorLLMResponse(response)

	if result.Type != CoordinatorTypePlan {
		t.Fatalf("expected plan, got %s", result.Type)
	}
	if result.Confidence != 0.85 {
		t.Fatalf("expected 0.85, got %f", result.Confidence)
	}
	if result.Reason != "需要多步骤规划" {
		t.Fatalf("expected '需要多步骤规划', got %s", result.Reason)
	}
	if len(result.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(result.Candidates))
	}
}

func TestCoordinatorLLMClassifier_ParseError(t *testing.T) {
	// 测试解析失败时的默认值
	response := `invalid json`

	result, _ := parseCoordinatorLLMResponse(response)

	if result.Type != CoordinatorTypeSupervisor {
		t.Fatalf("expected supervisor (default), got %s", result.Type)
	}
	if result.Confidence != 0.5 {
		t.Fatalf("expected 0.5, got %f", result.Confidence)
	}
}

func TestCoordinatorLLMClassifier_ConfidenceRange(t *testing.T) {
	// 测试置信度超出范围时的修正
	response := `{
		"type": "plan",
		"confidence": 1.5,
		"reason": "test",
		"candidates": []
	}`

	result, _ := parseCoordinatorLLMResponse(response)

	if result.Confidence != 0.5 {
		t.Fatalf("expected 0.5 (corrected), got %f", result.Confidence)
	}
}

func TestCoordinatorLLMClassifier_RenderPrompt(t *testing.T) {
	classifier := &CoordinatorLLMClassifierImpl{
		prompt: "{{.Summary}}\n{{.CurrentMessage}}",
	}

	input := &CoordinatorSelectInput{
		Summary:        "test summary",
		CurrentMessage: "test message",
		RecentMessages: []router.MessageContext{
			{Role: "user", Content: "hello"},
		},
	}

	prompt := classifier.renderPrompt(input, 0.75)

	if !contains(prompt, "test summary") || !contains(prompt, "test message") {
		t.Fatalf("prompt rendering failed: %s", prompt)
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

func TestConfigurableCoordinatorRuleMatcher_Reload(t *testing.T) {
	// 测试配置加载
	matcher, err := NewConfigurableCoordinatorRuleMatcher("configs/coordinator_rules.yaml")
	if err != nil {
		t.Skip("config file not found, skipping reload test")
	}

	// 测试热更新接口
	err = matcher.ReloadRules()
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
}

func TestConfigurableCoordinatorRuleMatcher_Match(t *testing.T) {
	matcher, err := NewConfigurableCoordinatorRuleMatcher("configs/coordinator_rules.yaml")
	if err != nil {
		t.Skip("config file not found, skipping match test")
	}

	input := &CoordinatorSelectInput{
		CurrentMessage: "请分步骤分析",
	}

	matches, err := matcher.Match(input, 0.5)
	if err != nil {
		t.Fatalf("match failed: %v", err)
	}

	if len(matches) == 0 {
		t.Fatal("expected at least one match")
	}
}
