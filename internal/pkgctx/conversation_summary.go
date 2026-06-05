package pkgctx

import (
	"fmt"
	"strings"
)

// FormatSummaryForRoute 生成路由用的简短摘要（当前对象、时间范围等）。
func FormatSummaryForRoute(summary *ConversationSummary) string {
	if summary == nil {
		return ""
	}
	var parts []string
	if summary.CurrentObject != "" {
		parts = append(parts, "当前对象: "+summary.CurrentObject)
	}
	if summary.TimeRange != "" {
		parts = append(parts, "时间范围: "+summary.TimeRange)
	}
	if len(summary.DocTypes) > 0 {
		parts = append(parts, "文档类型: "+summary.DocTypes[0])
	}
	return strings.Join(parts, "; ")
}

// FormatSummaryForPrompt 生成注入 LLM 的会话摘要块。
func FormatSummaryForPrompt(summary *ConversationSummary) string {
	if summary == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 本场会话摘要\n")
	b.WriteString("以下是本场对话已确认的背景信息，请结合最近消息理解用户意图。\n")

	hasContent := false
	if obj := strings.TrimSpace(summary.CurrentObject); obj != "" {
		fmt.Fprintf(&b, "\n- **当前分析对象**：%s\n", obj)
		hasContent = true
	}
	if tr := strings.TrimSpace(summary.TimeRange); tr != "" {
		fmt.Fprintf(&b, "- **时间范围**：%s\n", tr)
		hasContent = true
	}
	if len(summary.DocTypes) > 0 {
		fmt.Fprintf(&b, "- **文档类型**：%s\n", strings.Join(summary.DocTypes, "、"))
		hasContent = true
	}
	if facts := nonEmptyStrings(summary.ConfirmedFacts); len(facts) > 0 {
		b.WriteString("\n### 已确认事实\n")
		for i, fact := range facts {
			fmt.Fprintf(&b, "%d. %s\n", i+1, fact)
		}
		hasContent = true
	}
	if pending := nonEmptyStrings(summary.PendingQuestions); len(pending) > 0 {
		b.WriteString("\n### 待澄清问题\n")
		for i, q := range pending {
			fmt.Fprintf(&b, "%d. %s\n", i+1, q)
		}
		hasContent = true
	}
	if !hasContent {
		return ""
	}
	return strings.TrimSpace(b.String())
}

func nonEmptyStrings(items []string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}
