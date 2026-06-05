package theme

import (
	"context"
	"testing"
)

func TestService_Snapshot_AITheme(t *testing.T) {
	svc := NewService("../../configs/theme_registry.yaml", "../../configs/persona_daily_picks_universe.yaml", nil)
	resp, err := svc.Snapshot(context.Background(), "ai", "")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(resp.Themes) == 0 {
		t.Fatal("expected at least one ai theme snapshot")
	}
	foundCN, foundUS := false, false
	for _, th := range resp.Themes {
		if th.ID != "ai" {
			t.Errorf("unexpected theme id %s", th.ID)
		}
		if th.StockCount == 0 {
			t.Errorf("theme %s market %s has zero stocks", th.ID, th.Market)
		}
		if th.Market == "cn" {
			foundCN = true
		}
		if th.Market == "us" {
			foundUS = true
		}
		if th.State == "" {
			t.Errorf("missing state for %s/%s", th.ID, th.Market)
		}
	}
	if !foundUS {
		t.Error("expected us market snapshot for ai theme")
	}
	_ = foundCN
}

func TestService_Snapshot_EmbodiedAI(t *testing.T) {
	svc := NewService("../../configs/theme_registry.yaml", "../../configs/persona_daily_picks_universe.yaml", nil)
	resp, err := svc.Snapshot(context.Background(), "embodied_ai", "cn")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(resp.Themes) != 1 {
		t.Fatalf("expected 1 cn snapshot, got %d", len(resp.Themes))
	}
	if resp.Themes[0].StockCount < 2 {
		t.Errorf("expected multiple embodied_ai cn stocks, got %d", resp.Themes[0].StockCount)
	}
}

func TestClassifyState(t *testing.T) {
	cases := map[string]struct {
		avg, breadth float64
		want         string
	}{
		"overheated": {6, 85, "overheated"},
		"hot":        {3, 65, "hot"},
		"emerging":   {1, 50, "emerging"},
		"cooling":    {-2, 30, "cooling"},
		"neutral":    {0.5, 40, "neutral"},
	}
	for name, tc := range cases {
		if got := classifyState(tc.avg, tc.breadth); got != tc.want {
			t.Errorf("%s: want %s got %s", name, tc.want, got)
		}
	}
}
