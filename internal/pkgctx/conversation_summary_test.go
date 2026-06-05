package pkgctx

import (
	"strings"
	"testing"
)

func TestFormatSummaryForPrompt(t *testing.T) {
	text := FormatSummaryForPrompt(&ConversationSummary{
		CurrentObject:    "600519 贵州茅台",
		TimeRange:        "2024",
		ConfirmedFacts:   []string{"PE 约 25 倍", "PE 约 25 倍"},
		PendingQuestions: []string{"是否关注季度营收？"},
	})
	for _, want := range []string{"本场会话摘要", "600519", "已确认事实", "PE 约 25 倍", "待澄清问题"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %q", want, text)
		}
	}
}

func TestFormatSummaryForPromptEmpty(t *testing.T) {
	if FormatSummaryForPrompt(nil) != "" {
		t.Fatal("expected empty")
	}
}
