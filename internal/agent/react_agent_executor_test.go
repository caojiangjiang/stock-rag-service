package agent

import (
	"encoding/json"
	"testing"
)

func TestParseLLMResponse_FinalAnswer(t *testing.T) {
	resp := `思考: 已收集足够信息
行动: 总结
行动输入: 贵州茅台2024年营收为1555亿元。`
	thought, action, input, final := parseLLMResponse(resp)
	if !final {
		t.Fatalf("expected final answer, got action=%q", action)
	}
	if thought == "" {
		t.Fatal("expected thought")
	}
	if action != "总结" {
		t.Fatalf("action=%q", action)
	}
	if input == "" {
		t.Fatal("expected action input")
	}
}

func TestParseLLMResponse_ToolCall(t *testing.T) {
	resp := `思考: 需要检索公告
行动: search_announcements
行动输入: {"stock_code":"600519","query":"年报"}`
	thought, action, input, final := parseLLMResponse(resp)
	if final {
		t.Fatal("should not be final")
	}
	if action != "search_announcements" {
		t.Fatalf("action=%q", action)
	}
	if thought == "" {
		t.Fatal("expected thought")
	}
	if input == "" || input[0] != '{' {
		t.Fatalf("input=%q", input)
	}
}

func TestParseLLMResponse_MultilineActionInput(t *testing.T) {
	resp := `思考: 计算增长率
行动: calculator
行动输入: {
  "expression": "(1555-1347)/1347"
}`
	_, action, input, final := parseLLMResponse(resp)
	if final || action != "calculator" {
		t.Fatalf("action=%q final=%v", action, final)
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		t.Fatalf("invalid json input: %v raw=%q", err, input)
	}
}

func TestIsFinalAction(t *testing.T) {
	cases := []struct {
		action string
		want   bool
	}{
		{"总结", true},
		{"finish", true},
		{"search_announcements", false},
	}
	for _, tc := range cases {
		if got := isFinalAction(tc.action); got != tc.want {
			t.Fatalf("action %q: got %v want %v", tc.action, got, tc.want)
		}
	}
}

func TestFormatObservation(t *testing.T) {
	obs := formatObservation(ToolObservation{
		Step:   1,
		Tool:   "calculator",
		Status: "success",
		Result: map[string]interface{}{"value": 42},
	})
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(obs), &parsed); err != nil {
		t.Fatalf("observation not valid json: %v", err)
	}
	if parsed["status"] != "success" {
		t.Fatalf("status=%v", parsed["status"])
	}
}

func TestFinalizeAnswer(t *testing.T) {
	if got := finalizeAnswer("思考内容", "最终答案"); got != "最终答案" {
		t.Fatalf("got %q", got)
	}
	if got := finalizeAnswer("仅思考", ""); got != "仅思考" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveExecuteMode(t *testing.T) {
	mode := resolveExecuteMode(&ExecuteRequest{Mode: "rag"})
	if mode != "rag" {
		t.Fatalf("mode=%q", mode)
	}
}
