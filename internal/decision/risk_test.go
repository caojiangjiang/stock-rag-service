package decision

import (
	"testing"

	"stock_rag/internal/portfolio"
	"stock_rag/internal/theme"
)

func TestRiskRules_Evaluate(t *testing.T) {
	cfg := &RiskRules{}
	cfg.Rules.MaxSinglePositionWeightPct = 25
	cfg.Rules.MaxThemeWeightPct = 40
	cfg.Rules.RequireThesisForStock = true
	cfg.Rules.MinThesisLength = 8
	cfg.Rules.WarnUnrealizedLossPct = 15

	summary := &portfolio.Summary{
		Positions: []portfolio.PositionView{
			{
				Position: portfolio.Position{
					AssetType: portfolio.AssetStock,
					StockCode: "600519",
					StockName: "茅台",
					Thesis:    "长期",
				},
				HasQuote:      true,
				WeightPct:     30,
				UnrealizedPct: -20,
				QuoteSource:   "mock",
			},
		},
	}
	exposures := []ThemeExposure{
		{ThemeID: "ai", ThemeName: "AI", State: "cooling", WeightPct: 30},
	}
	report := cfg.Evaluate(summary, exposures)
	if report.Passed {
		t.Fatal("expected violations")
	}
	if len(report.Violations) < 3 {
		t.Fatalf("expected multiple violations, got %d", len(report.Violations))
	}
}

func TestBuildThemeExposures(t *testing.T) {
	summary := &portfolio.Summary{
		Positions: []portfolio.PositionView{
			{
				Position:  portfolio.Position{AssetType: portfolio.AssetStock, StockCode: "688256", Market: "cn"},
				HasQuote:  true,
				WeightPct: 60,
			},
			{
				Position:  portfolio.Position{AssetType: portfolio.AssetStock, StockCode: "NVDA", Market: "us"},
				HasQuote:  true,
				WeightPct: 40,
			},
		},
	}
	index := map[string][]theme.SymbolThemeMembership{
		"cn:688256": {{ThemeID: "ai", ThemeName: "AI人工智能", Market: "cn"}},
		"us:NVDA":   {{ThemeID: "ai", ThemeName: "AI人工智能", Market: "us"}},
	}
	snap := &theme.SnapshotResponse{
		Themes: []theme.ThemeSnapshot{
			{ID: "ai", Market: "cn", State: "hot", AvgChangePct: 2.5},
			{ID: "ai", Market: "us", State: "emerging", AvgChangePct: 1.2},
		},
	}
	exposures := BuildThemeExposures(summary, snap, index)
	if len(exposures) != 2 {
		t.Fatalf("expected 2 exposures, got %d", len(exposures))
	}
	if exposures[0].WeightPct != 60 {
		t.Fatalf("expected top exposure 60%%, got %v", exposures[0].WeightPct)
	}
}
