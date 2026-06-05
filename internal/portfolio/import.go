package portfolio

import (
	"context"
	"fmt"
	"math"
	"strings"

	"stock_rag/internal/market"
)

// ComputeFundImport 根据 App 展示的金额/收益反推份额与成本单位净值。
func ComputeFundImport(req FundAppImportRequest, nav float64) (*FundImportPreview, error) {
	code := strings.TrimSpace(req.StockCode)
	if code == "" {
		return nil, fmt.Errorf("stock_code is required")
	}
	if req.MarketValue <= 0 {
		return nil, fmt.Errorf("market_value must be positive")
	}
	if nav <= 0 {
		return nil, fmt.Errorf("unit_nav must be positive")
	}

	qty := req.MarketValue / nav
	costTotal := req.MarketValue - req.HoldingPnL
	if costTotal <= 0 {
		return nil, fmt.Errorf("invalid holding_pnl: cost would be non-positive")
	}
	costPrice := costTotal / qty

	pnlPct := 0.0
	if costTotal > 0 {
		pnlPct = req.HoldingPnL / costTotal * 100
	}

	if req.HoldingPnLPct != 0 {
		diff := math.Abs(pnlPct - req.HoldingPnLPct)
		if diff > 1.0 {
			return nil, fmt.Errorf("holding_pnl_pct mismatch: computed %.2f%%, got %.2f%%", pnlPct, req.HoldingPnLPct)
		}
	}

	return &FundImportPreview{
		StockCode:     code,
		StockName:     strings.TrimSpace(req.StockName),
		Quantity:      round4(qty),
		CostPrice:     round4(costPrice),
		CostTotal:     round2(costTotal),
		MarketValue:   round2(req.MarketValue),
		HoldingPnL:    round2(req.HoldingPnL),
		HoldingPnLPct: round2(pnlPct),
		UnitNAV:       round4(nav),
	}, nil
}

func (s *Service) resolveUnitNAV(code string, override float64) (float64, error) {
	if override > 0 {
		return override, nil
	}
	if f, ok := s.market.(market.FundNAVFetcher); ok {
		q := f.GetFundNAV(code)
		if q.HasQuote && q.Price > 0 {
			return q.Price, nil
		}
	}
	q := s.market.GetQuote(code)
	if !q.HasQuote || q.Price <= 0 {
		return 0, fmt.Errorf("unit_nav unavailable for %s: enter manually or check fund code", code)
	}
	return q.Price, nil
}

// PreviewFundImport 预览基金导入计算结果。
func (s *Service) PreviewFundImport(req FundAppImportRequest) (*FundImportPreview, error) {
	nav, err := s.resolveUnitNAV(req.StockCode, req.UnitNAV)
	if err != nil {
		return nil, err
	}
	return ComputeFundImport(req, nav)
}

// ImportFundFromApp 从 App 字段导入并 upsert 基金持仓。
func (s *Service) ImportFundFromApp(ctx context.Context, userID string, req FundAppImportRequest) (*Position, *FundImportPreview, error) {
	preview, err := s.PreviewFundImport(req)
	if err != nil {
		return nil, nil, err
	}
	pos, err := s.Upsert(ctx, userID, UpsertRequest{
		AssetType: AssetFund,
		StockCode: preview.StockCode,
		StockName: preview.StockName,
		Market:    "cn",
		Quantity:  preview.Quantity,
		CostPrice: preview.CostPrice,
		Currency:  "CNY",
		Thesis:    req.Thesis,
	})
	if err != nil {
		return nil, nil, err
	}
	return pos, preview, nil
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}
