package long_test

import (
	"strings"
	"testing"

	"stock_rag/internal/memory/long"
)

func TestFormatPromptContextEmpty(t *testing.T) {
	if got := long.FormatPromptContext(nil, nil); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestFormatPromptContextPreferencesAndInsights(t *testing.T) {
	text := long.FormatPromptContext(&long.UserPreferences{
		DetailLevel:      long.DetailLevelBrief,
		RiskAppetite:     long.RiskAppetiteLow,
		InteractionStyle: "偏数据",
		InterestedStocks: []string{"600519", "600519"},
		PreferredMetrics: []string{"PE", "ROE"},
	}, []*long.Insight{
		{Summary: "用户曾关注茅台估值"},
		nil,
		{Content: "宁德时代产业链分析"},
	})
	for _, want := range []string{
		"用户长期记忆",
		"简洁",
		"保守",
		"偏数据",
		"600519",
		"PE",
		"用户曾关注茅台估值",
		"宁德时代产业链分析",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}
