package decision

import (
	"sort"
	"strings"

	"stock_rag/internal/portfolio"
	"stock_rag/internal/theme"
)

// BuildThemeExposures 将持仓映射到主题暴露。
func BuildThemeExposures(summary *portfolio.Summary, themeSnap *theme.SnapshotResponse, index map[string][]theme.SymbolThemeMembership) []ThemeExposure {
	if summary == nil || len(summary.Positions) == 0 {
		return nil
	}

	snapByKey := make(map[string]theme.ThemeSnapshot, len(themeSnap.Themes))
	for _, th := range themeSnap.Themes {
		snapByKey[th.ID+":"+th.Market] = th
	}

	type agg struct {
		themeID, themeName, market, state string
		avgChange                         float64
		weight                            float64
		symbols                           map[string]struct{}
	}
	merged := make(map[string]*agg)

	for _, p := range summary.Positions {
		if normalizeAsset(p.AssetType) != portfolio.AssetStock || !p.HasQuote {
			continue
		}
		key := positionKey(p.Market, p.StockCode)
		for _, ref := range index[key] {
			aggKey := ref.ThemeID + ":" + ref.Market
			a := merged[aggKey]
			if a == nil {
				state := "unknown"
				avg := 0.0
				if snap, ok := snapByKey[aggKey]; ok {
					state = snap.State
					avg = snap.AvgChangePct
				}
				a = &agg{
					themeID:   ref.ThemeID,
					themeName: ref.ThemeName,
					market:    ref.Market,
					state:     state,
					avgChange: avg,
					symbols:   make(map[string]struct{}),
				}
				merged[aggKey] = a
			}
			a.weight += p.WeightPct
			a.symbols[p.StockCode] = struct{}{}
		}
	}

	out := make([]ThemeExposure, 0, len(merged))
	for _, a := range merged {
		if a.weight < 0.01 {
			continue
		}
		symbols := make([]string, 0, len(a.symbols))
		for s := range a.symbols {
			symbols = append(symbols, s)
		}
		sort.Strings(symbols)

		exp := ThemeExposure{
			ThemeID:      a.themeID,
			ThemeName:    a.themeName,
			Market:       a.market,
			State:        a.state,
			AvgChangePct: a.avgChange,
			WeightPct:    round2(a.weight),
			Symbols:      symbols,
		}
		if a.state == "cooling" && a.weight >= 10 {
			exp.Alert = "主题退潮且暴露偏高，考虑降低仓位"
		} else if a.state == "overheated" && a.weight >= 15 {
			exp.Alert = "主题过热，注意追高风险"
		}
		out = append(out, exp)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].WeightPct > out[j].WeightPct
	})
	return out
}

func positionKey(market, symbol string) string {
	return strings.ToLower(strings.TrimSpace(market)) + ":" + strings.ToUpper(strings.TrimSpace(symbol))
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}
