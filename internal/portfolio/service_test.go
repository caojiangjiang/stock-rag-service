package portfolio

import (
	"context"
	"testing"
)

func TestMemoryStore_UpsertAndSummary(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(store, nil)
	ctx := context.Background()
	userID := "user-1"

	pos, err := svc.Upsert(ctx, userID, UpsertRequest{
		StockCode: "600519",
		StockName: "贵州茅台",
		Market:    "cn",
		Quantity:  100,
		CostPrice: 1600,
		Thesis:    "长期持有",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if pos.ID == "" {
		t.Fatal("expected position id")
	}

	summary, err := svc.Summary(ctx, userID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.PositionCount != 1 {
		t.Fatalf("expected 1 position, got %d", summary.PositionCount)
	}
	if summary.TotalCost != 160000 {
		t.Errorf("expected cost 160000, got %v", summary.TotalCost)
	}
	if !summary.Positions[0].HasQuote {
		t.Error("expected quote for 600519")
	}
	if summary.Positions[0].MarketValue <= 0 {
		t.Error("expected positive market value")
	}
}

func TestMemoryStore_UpsertValidation(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(store, nil)
	_, err := svc.Upsert(context.Background(), "u1", UpsertRequest{StockCode: "", Quantity: 1, CostPrice: 1})
	if err == nil {
		t.Fatal("expected error for empty stock code")
	}
}

func TestMemoryStore_FundPosition(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(store, nil)
	ctx := context.Background()
	userID := "user-fund"

	_, err := svc.Upsert(ctx, userID, UpsertRequest{
		AssetType: AssetFund,
		StockCode: "005827",
		StockName: "易方达蓝筹精选",
		Market:    "cn",
		Quantity:  10000,
		CostPrice: 2.05,
		Thesis:    "长期定投",
	})
	if err != nil {
		t.Fatalf("upsert fund: %v", err)
	}

	summary, err := svc.Summary(ctx, userID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.PositionCount != 1 {
		t.Fatalf("expected 1 position, got %d", summary.PositionCount)
	}
	if summary.Positions[0].AssetType != AssetFund {
		t.Errorf("expected asset_type fund, got %s", summary.Positions[0].AssetType)
	}
	if !summary.Positions[0].HasQuote {
		t.Error("expected quote for fund 005827")
	}
	if summary.Positions[0].MarketValue <= 0 {
		t.Error("expected positive market value for fund")
	}
}
