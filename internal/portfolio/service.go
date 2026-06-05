package portfolio

import (
	"context"
	"strings"
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

	quoteMap := s.batchQuotes(ctx, positions)
	mock := market.NewDefaultProvider()

	views := make([]PositionView, 0, len(positions))
	var totalCost, totalValue float64

	for _, p := range positions {
		view := enrichPositionWithMap(p, quoteMap, mock, s.market)
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

func (s *Service) batchQuotes(ctx context.Context, positions []Position) map[string]market.Quote {
	var refs []market.SymbolRef
	for _, p := range positions {
		if normalizeAssetType(p.AssetType) == AssetFund {
			continue
		}
		mkt := strings.ToLower(strings.TrimSpace(p.Market))
		if mkt == "" {
			mkt = "cn"
		}
		refs = append(refs, market.SymbolRef{Code: p.StockCode, Market: mkt})
	}
	if len(refs) == 0 {
		return nil
	}
	return market.BatchFetchStockQuotes(ctx, refs)
}

func enrichPositionWithMap(p Position, quoteMap map[string]market.Quote, mock *market.DefaultProvider, provider market.Provider) PositionView {
	q := quoteForPositionMap(p, quoteMap, mock, provider)
	costValue := p.Quantity * p.CostPrice
	view := PositionView{
		Position:    p,
		CostValue:   round2(costValue),
		HasQuote:    q.HasQuote,
		QuoteSource: normalizeQuoteSource(q),
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

func quoteForPositionMap(p Position, quoteMap map[string]market.Quote, mock *market.DefaultProvider, provider market.Provider) market.Quote {
	if normalizeAssetType(p.AssetType) == AssetFund {
		if f, ok := provider.(market.FundNAVFetcher); ok {
			return f.GetFundNAV(p.StockCode)
		}
	}
	mkt := strings.ToLower(strings.TrimSpace(p.Market))
	if mkt == "" {
		mkt = "cn"
	}
	key := market.QuoteMapKey(mkt, p.StockCode)
	if quoteMap != nil {
		if q, ok := quoteMap[key]; ok && q.HasQuote {
			return q
		}
	}
	if mock != nil {
		if q := mock.GetQuote(p.StockCode); q.HasQuote {
			q.Source = "mock"
			return q
		}
	}
	return market.Quote{StockCode: p.StockCode, HasQuote: false}
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

func normalizeQuoteSource(q market.Quote) string {
	if !q.HasQuote {
		return "missing"
	}
	src := strings.ToLower(strings.TrimSpace(q.Source))
	if src == "" {
		return "unknown"
	}
	return src
}
