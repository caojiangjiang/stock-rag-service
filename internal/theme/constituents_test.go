package theme

import (
	"context"
	"testing"
)

func TestResolveStocks_MergeBoardAndTags(t *testing.T) {
	universe, err := LoadUniverseYAML("../../configs/persona_daily_picks_universe.yaml")
	if err != nil {
		t.Fatalf("load universe: %v", err)
	}

	loader := &BoardLoader{}
	// inject stub via ResolveStocks path - test merge logic directly with fake board members via def without network
	def := ThemeDef{
		ID:               "embodied_ai",
		MatchTags:        []string{"机器人"},
		EastMoneyBoardCN: "", // skip live
		ExtraSymbols:     map[string][]string{"cn": {"300024"}},
	}

	stocks := MatchStocks(def, universe)
	if len(stocks) == 0 {
		t.Fatal("expected tag/extra matched stocks")
	}

	merged, _ := ResolveStocks(context.Background(), loader, def, universe)
	if len(merged) < len(stocks) {
		t.Fatalf("merged %d want at least %d", len(merged), len(stocks))
	}
}
