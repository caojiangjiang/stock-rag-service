package portfolio

import (
	"context"
	"time"

	"stock_rag/internal/market"
)

// Service 持仓业务服务。
type Service struct {
	store  Store
	market market.Provider
}

func NewService(store Store, provider market.Provider) *Service {
	if provider == nil {
		provider = market.NewDefaultProvider()
	}
	return &Service{store: store, market: provider}
}

func (s *Service) Init(ctx context.Context) error {
	if s.store == nil {
		return nil
	}
	return s.store.Init(ctx)
}

func (s *Service) List(ctx context.Context, userID string) ([]Position, error) {
	return s.store.ListByUser(ctx, userID)
}

func (s *Service) Upsert(ctx context.Context, userID string, req UpsertRequest) (*Position, error) {
	return s.store.Upsert(ctx, userID, req)
}

func (s *Service) Delete(ctx context.Context, userID, id string) error {
	return s.store.Delete(ctx, userID, id)
}

func (s *Service) Summary(ctx context.Context, userID string) (*Summary, error) {
	positions, err := s.store.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	views := make([]PositionView, 0, len(positions))
	var totalCost, totalValue float64

	for _, p := range positions {
		view := enrichPosition(p, s.market)
		totalCost += view.CostValue
		totalValue += view.MarketValue
		views = append(views, view)
	}

	for i := range views {
		if totalValue > 0 && views[i].HasQuote {
			views[i].WeightPct = round2(views[i].MarketValue / totalValue * 100)
		}
	}

	pnl := totalValue - totalCost
	pnlPct := 0.0
	if totalCost > 0 {
		pnlPct = pnl / totalCost * 100
	}

	return &Summary{
		PositionCount: len(views),
		TotalCost:     round2(totalCost),
		TotalValue:    round2(totalValue),
		UnrealizedPnL: round2(pnl),
		UnrealizedPct: round2(pnlPct),
		Positions:     views,
		AsOf:          time.Now(),
	}, nil
}

func enrichPosition(p Position, provider market.Provider) PositionView {
	q := quoteForPosition(p, provider)
	costValue := p.Quantity * p.CostPrice
	view := PositionView{
		Position:  p,
		CostValue: round2(costValue),
		HasQuote:  q.HasQuote,
	}
	if q.HasQuote {
		view.CurrentPrice = q.Price
		view.ChangePercent = q.ChangePercent
		view.MarketValue = round2(p.Quantity * q.Price)
		view.UnrealizedPnL = round2(view.MarketValue - costValue)
		if costValue > 0 {
			view.UnrealizedPct = round2(view.UnrealizedPnL / costValue * 100)
		}
		if p.StockName == "" {
			view.StockName = q.StockName
		}
	}
	return view
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func quoteForPosition(p Position, provider market.Provider) market.Quote {
	if normalizeAssetType(p.AssetType) == AssetFund {
		if f, ok := provider.(market.FundNAVFetcher); ok {
			return f.GetFundNAV(p.StockCode)
		}
	}
	return provider.GetQuote(p.StockCode)
}
