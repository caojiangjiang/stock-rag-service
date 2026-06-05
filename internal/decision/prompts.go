package decision

import (
	"fmt"
	"strings"

	"stock_rag/internal/portfolio"
)

func buildPromptTemplates(summary *portfolio.Summary, exposures []ThemeExposure, risk RiskReport) []PromptTemplate {
	var templates []PromptTemplate

	// 1. 组合风险总览
	var posLines []string
	if summary != nil {
		for _, p := range summary.Positions {
			if !p.HasQuote {
				continue
			}
			posLines = append(posLines, fmt.Sprintf("- %s(%s): 仓位%.1f%%, 今日%.2f%%, 浮盈%.1f%%",
				firstNonEmpty(p.StockName, p.StockCode), p.StockCode, p.WeightPct, p.ChangePercent, p.UnrealizedPct))
		}
	}
	riskLines := "当前无风险规则告警。"
	if len(risk.Violations) > 0 {
		parts := make([]string, 0, len(risk.Violations))
		for _, v := range risk.Violations {
			parts = append(parts, v.Message)
		}
		riskLines = strings.Join(parts, "\n")
	}
	templates = append(templates, PromptTemplate{
		ID:    "portfolio_risk",
		Title: "组合风险审查",
		Text: fmt.Sprintf(`请基于以下真实持仓数据，评估当前组合风险（不要给具体买卖建议，列出风险点与需核实的问题）：

%s

风险规则检查结果：
%s

请从：集中度、主题暴露、浮亏、缺失 thesis 四方面给出 3-5 条要点。`, strings.Join(posLines, "\n"), riskLines),
	})

	// 2. 主题暴露
	if len(exposures) > 0 {
		var expLines []string
		for _, e := range exposures {
			line := fmt.Sprintf("- %s(%s): 暴露%.1f%%, 主题状态=%s, 平均涨跌%.2f%%",
				e.ThemeName, e.Market, e.WeightPct, e.State, e.AvgChangePct)
			if e.Alert != "" {
				line += "，" + e.Alert
			}
			expLines = append(expLines, line)
		}
		templates = append(templates, PromptTemplate{
			ID:    "theme_exposure",
			Title: "主题暴露分析",
			Text: fmt.Sprintf(`我的持仓在以下投资主题上有暴露：

%s

请分析：这些主题当前状态对我的组合意味着什么？有哪些同步/对冲关系？`, strings.Join(expLines, "\n")),
		})
	}

	// 3. 加仓模板（取最大仓位标的）
	if summary != nil && len(summary.Positions) > 0 {
		top := summary.Positions[0]
		for _, p := range summary.Positions {
			if p.WeightPct > top.WeightPct {
				top = p
			}
		}
		thesis := strings.TrimSpace(top.Thesis)
		fals := strings.TrimSpace(top.Falsification)
		templates = append(templates, PromptTemplate{
			ID:    "add_position",
			Title: fmt.Sprintf("是否加仓 %s", firstNonEmpty(top.StockName, top.StockCode)),
			Text: fmt.Sprintf(`我在考虑加仓 %s(%s)，当前仓位 %.1f%%，成本 %.2f，现价 %.2f，浮盈 %.1f%%。

买入逻辑：%s
证伪条件：%s

请从正反两方面分析：支持加仓的理由、不支持的理由、我需要补充哪些信息才能做决定。`, 
				firstNonEmpty(top.StockName, top.StockCode), top.StockCode, top.WeightPct,
				top.CostPrice, top.CurrentPrice, top.UnrealizedPct,
				orDefault(thesis, "（未填写）"), orDefault(fals, "（未填写）")),
		})
	}

	// 4. 缺失 thesis 的持仓
	if summary != nil {
		var missing []string
		for _, p := range summary.Positions {
			if normalizeAsset(p.AssetType) == portfolio.AssetStock && len(strings.TrimSpace(p.Thesis)) < 8 {
				missing = append(missing, p.StockCode)
			}
		}
		if len(missing) > 0 {
			templates = append(templates, PromptTemplate{
				ID:    "thesis_gap",
				Title: "补全投资逻辑",
				Text: fmt.Sprintf(`以下持仓缺少清晰买入逻辑：%s

请帮我针对每只票列出：应关注的核心指标、需要验证的假设、可能的证伪信号。`, strings.Join(missing, "、")),
			})
		}
	}

	return templates
}

func buildWatchItems(summary *portfolio.Summary, exposures []ThemeExposure, risk RiskReport, trust DataTrustSummary) []WatchItem {
	var items []WatchItem

	if trust.QuoteMock > 0 || trust.QuoteMissing > 0 {
		items = append(items, WatchItem{
			Severity: "warn",
			Title:    "行情数据不完整",
			Detail:   fmt.Sprintf("live=%d mock=%d 缺失=%d，决策前请点「刷新」或确认数据源", trust.QuoteLive, trust.QuoteMock, trust.QuoteMissing),
		})
	}
	for _, v := range risk.Violations {
		if v.RuleID == "max_single_position_weight" || v.RuleID == "max_theme_weight" {
			items = append(items, WatchItem{
				Severity: "warn",
				Title:    "集中度偏高",
				Detail:   v.Message,
				Symbols:  nonEmptySymbol(v.Symbol),
			})
		}
	}
	for _, e := range exposures {
		if e.Alert != "" {
			items = append(items, WatchItem{
				Severity: "warn",
				Title:    e.ThemeName + " 主题预警",
				Detail:   e.Alert + fmt.Sprintf("（暴露 %.1f%%）", e.WeightPct),
				Symbols:  e.Symbols,
			})
		}
	}
	if summary != nil {
		for _, p := range summary.Positions {
			if p.HasQuote && p.UnrealizedPct <= -15 {
				items = append(items, WatchItem{
					Severity: "critical",
					Title:    label(p) + " 浮亏较大",
					Detail:   fmt.Sprintf("浮亏 %.1f%%，请复核 thesis 是否仍成立", p.UnrealizedPct),
					Symbols:  []string{p.StockCode},
				})
			}
		}
	}
	if len(items) == 0 {
		items = append(items, WatchItem{
			Severity: "info",
			Title:    "暂无紧急事项",
			Detail:   "可查看主题暴露与固定模板，发起深度追问",
		})
	}
	if len(items) > 5 {
		items = items[:5]
	}
	return items
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func nonEmptySymbol(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}
