package portfolio

import (
	"context"
	"math"
	"testing"
)

func TestComputeFundImport_Screenshot003095(t *testing.T) {
	preview, err := ComputeFundImport(FundAppImportRequest{
		StockCode:     "003095",
		StockName:     "中欧医疗健康混合A",
		MarketValue:   80962.37,
		HoldingPnL:    -32762.71,
		UnitNAV:       1.5450,
		HoldingPnLPct: -28.74,
	}, 1.5450)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}

	if preview.Quantity <= 0 || preview.CostPrice <= 0 {
		t.Fatalf("invalid preview: %+v", preview)
	}

	mv := preview.Quantity * preview.UnitNAV
	if math.Abs(mv-preview.MarketValue) > 1 {
		t.Errorf("market value drift: got %.2f want %.2f", mv, preview.MarketValue)
	}

	cost := preview.Quantity * preview.CostPrice
	pnl := mv - cost
	if math.Abs(pnl-preview.HoldingPnL) > 5 {
		t.Errorf("pnl drift: got %.2f want %.2f", pnl, preview.HoldingPnL)
	}

	if math.Abs(preview.HoldingPnLPct+28.74) > 0.5 {
		t.Errorf("pnl pct: got %.2f", preview.HoldingPnLPct)
	}
}

func TestImportFundFromApp(t *testing.T) {
	svc := NewService(NewMemoryStore(), nil)
	ctx := context.Background()

	pos, preview, err := svc.ImportFundFromApp(ctx, "u1", FundAppImportRequest{
		StockCode:   "003095",
		StockName:   "中欧医疗健康混合A",
		MarketValue: 80962.37,
		HoldingPnL:  -32762.71,
		UnitNAV:     1.5450,
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if pos.AssetType != AssetFund {
		t.Errorf("expected fund, got %s", pos.AssetType)
	}
	if preview.Quantity <= 0 {
		t.Error("expected positive quantity")
	}
}
