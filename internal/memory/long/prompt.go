package long

import (
	"fmt"
	"strings"
)

const defaultInsightPreviewRunes = 200

// FormatPromptContext 将用户偏好与相关洞察格式化为可注入 system/user 上下文的文本。
func FormatPromptContext(prefs *UserPreferences, insights []*Insight) string {
	if prefs == nil && len(insights) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## 用户长期记忆\n")
	b.WriteString("以下为用户跨会话的偏好与历史洞察，回答时请适当参考，但不要生硬复述。\n")

	if prefs != nil {
		writePreferences(&b, prefs)
	}
	if len(insights) > 0 {
		writeInsights(&b, insights)
	}
	return strings.TrimSpace(b.String())
}

func writePreferences(b *strings.Builder, prefs *UserPreferences) {
	b.WriteString("\n### 对话偏好\n")
	switch prefs.DetailLevel {
	case DetailLevelBrief:
		b.WriteString("- 回答详略：简洁，优先结论与要点\n")
	case DetailLevelDetail:
		b.WriteString("- 回答详略：详细，可展开分析与数据\n")
	default:
		b.WriteString("- 回答详略：标准\n")
	}
	switch prefs.RiskAppetite {
	case RiskAppetiteHigh:
		b.WriteString("- 风险偏好：激进，可讨论高波动机会\n")
	case RiskAppetiteLow:
		b.WriteString("- 风险偏好：保守，强调风险与下行保护\n")
	default:
		b.WriteString("- 风险偏好：均衡\n")
	}
	if style := strings.TrimSpace(prefs.InteractionStyle); style != "" {
		b.WriteString("- 交互风格：" + style + "\n")
	}
	if stocks := joinNonEmpty(prefs.InterestedStocks); stocks != "" {
		b.WriteString("- 关注标的：" + stocks + "\n")
	}
	if metrics := joinNonEmpty(prefs.PreferredMetrics); metrics != "" {
		b.WriteString("- 偏好指标：" + metrics + "\n")
	}
}

func writeInsights(b *strings.Builder, insights []*Insight) {
	b.WriteString("\n### 相关历史洞察\n")
	idx := 0
	for _, ins := range insights {
		if ins == nil {
			continue
		}
		text := strings.TrimSpace(ins.Summary)
		if text == "" {
			text = truncateRunes(strings.TrimSpace(ins.Content), defaultInsightPreviewRunes)
		}
		if text == "" {
			continue
		}
		idx++
		fmt.Fprintf(b, "%d. %s\n", idx, text)
	}
}

func joinNonEmpty(items []string) string {
	var parts []string
	seen := make(map[string]bool)
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		parts = append(parts, item)
	}
	return strings.Join(parts, "、")
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
