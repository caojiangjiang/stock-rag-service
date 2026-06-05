package theme

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"stock_rag/internal/market"
	personamodel "stock_rag/internal/persona/model"
)

// Service 主题快照服务。
type Service struct {
	registryPath string
	universePath string
	market       market.Provider
	boards       *BoardLoader
	loadRegistry func(string) (*Registry, error)
	loadUniverse func(string) (*personamodel.UniverseConfig, error)
	mockFallback *market.DefaultProvider
	cacheMu      sync.RWMutex
	cache        map[string]snapshotCacheEntry
	cacheTTL     time.Duration
}

// NewService 创建主题快照服务。
func NewService(registryPath, universePath string, provider market.Provider) *Service {
	if provider == nil {
		provider = market.NewDefaultProvider()
	}
	return &Service{
		registryPath: registryPath,
		universePath: universePath,
		market:       provider,
		boards:       NewBoardLoader(),
		loadRegistry: LoadRegistry,
		loadUniverse: LoadUniverseYAML,
		mockFallback: market.NewDefaultProvider(),
		cache:        make(map[string]snapshotCacheEntry),
		cacheTTL:     parseSnapshotCacheTTL(),
	}
}

// Snapshot 返回主题快照列表（优先读缓存，避免频繁请求东财）。
func (s *Service) Snapshot(ctx context.Context, themeID, marketFilter string) (*SnapshotResponse, error) {
	var entry *snapshotCacheEntry
	var fromCache bool
	if e, ok := s.cacheGet(); ok {
		entry = e
		fromCache = true
	} else {
		resp, err := s.buildSnapshotFresh(ctx)
		if err != nil {
			return nil, err
		}
		s.cacheSet(resp)
		entry = &snapshotCacheEntry{resp: resp, at: time.Now()}
	}

	out := filterSnapshot(entry.resp, themeID, marketFilter, fromCache, entry.at)
	if themeID != "" && len(out.Themes) == 0 {
		return nil, fmt.Errorf("theme not found: %s", themeID)
	}
	return out, nil
}

// RefreshSnapshot 强制刷新并更新缓存。
func (s *Service) RefreshSnapshot(ctx context.Context, themeID, marketFilter string) (*SnapshotResponse, error) {
	s.invalidateCache()
	resp, err := s.buildSnapshotFresh(ctx)
	if err != nil {
		return nil, err
	}
	s.cacheSet(resp)
	out := filterSnapshot(resp, themeID, marketFilter, false, time.Now())
	if themeID != "" && len(out.Themes) == 0 {
		return nil, fmt.Errorf("theme not found: %s", themeID)
	}
	return out, nil
}

func (s *Service) buildSnapshotFresh(ctx context.Context) (*SnapshotResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	reg, err := s.loadRegistry(s.registryPath)
	if err != nil {
		return nil, err
	}
	universe, err := s.loadUniverse(s.universePath)
	if err != nil {
		return nil, fmt.Errorf("load universe: %w", err)
	}

	defs := reg.Themes

	quoteMap, resolved := s.collectQuotes(ctx, defs, universe)

	now := time.Now()
	resp := &SnapshotResponse{AsOf: now}
	for _, rt := range resolved {
		snaps := s.buildSnapshots(rt.def, rt.stocks, "", quoteMap)
		resp.Themes = append(resp.Themes, snaps...)
	}
	return resp, nil
}

type resolvedTheme struct {
	def    ThemeDef
	stocks []personamodel.CandidateStock
}

func (s *Service) collectQuotes(ctx context.Context, defs []ThemeDef, universe *personamodel.UniverseConfig) (map[string]market.Quote, []resolvedTheme) {
	quoteMap := make(map[string]market.Quote)
	stockByKey := make(map[string]personamodel.CandidateStock)
	refSeen := make(map[string]bool)
	var refs []market.SymbolRef
	var resolved []resolvedTheme

	addRef := func(marketName, code string) {
		if code == "" {
			return
		}
		key := market.QuoteMapKey(marketName, code)
		if refSeen[key] {
			return
		}
		if q, ok := quoteMap[key]; ok && q.HasQuote {
			return
		}
		refSeen[key] = true
		refs = append(refs, market.SymbolRef{Code: code, Market: marketName})
	}

	for _, def := range defs {
		stocks, boardQuotes := ResolveStocks(ctx, s.boards, def, universe)
		resolved = append(resolved, resolvedTheme{def: def, stocks: stocks})
		for k, q := range boardQuotes {
			if q.HasQuote {
				quoteMap[k] = q
			}
		}
		for _, st := range stocks {
			key := market.QuoteMapKey(st.Market, st.Symbol)
			stockByKey[key] = st
			addRef(st.Market, st.Symbol)
		}
		addRef("cn", def.BenchmarkCN)
		addRef("us", def.BenchmarkUS)
	}

	batch := market.BatchFetchStockQuotes(ctx, refs)
	for k, q := range batch {
		if q.HasQuote {
			quoteMap[k] = q
		}
	}

	for key, st := range stockByKey {
		if q, ok := quoteMap[key]; ok && q.HasQuote {
			continue
		}
		if q := s.mockFallback.GetQuote(st.Symbol); q.HasQuote {
			q.Source = "mock"
			quoteMap[key] = q
		}
	}
	for _, def := range defs {
		for _, bench := range []struct {
			mkt  string
			code string
		}{
			{"cn", def.BenchmarkCN},
			{"us", def.BenchmarkUS},
		} {
			key := market.QuoteMapKey(bench.mkt, bench.code)
			if q, ok := quoteMap[key]; ok && q.HasQuote {
				continue
			}
			if q := s.mockFallback.GetQuote(bench.code); q.HasQuote {
				q.Source = "mock"
				quoteMap[key] = q
			}
		}
	}
	return quoteMap, resolved
}

func (s *Service) buildSnapshots(def ThemeDef, stocks []personamodel.CandidateStock, marketFilter string, quoteMap map[string]market.Quote) []ThemeSnapshot {
	if len(stocks) == 0 {
		return nil
	}

	byMarket := map[string][]personamodel.CandidateStock{}
	for _, st := range stocks {
		m := strings.ToLower(st.Market)
		if marketFilter != "" && m != strings.ToLower(marketFilter) {
			continue
		}
		byMarket[m] = append(byMarket[m], st)
	}

	var out []ThemeSnapshot
	for m, list := range byMarket {
		if len(list) == 0 {
			continue
		}
		bench := def.BenchmarkCN
		if m == "us" {
			bench = def.BenchmarkUS
		}
		out = append(out, s.aggregateTheme(def, m, list, bench, quoteMap))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Market < out[j].Market })
	return out
}

func (s *Service) aggregateTheme(def ThemeDef, marketName string, stocks []personamodel.CandidateStock, benchmarkCode string, quotes map[string]market.Quote) ThemeSnapshot {
	themeStocks := make([]ThemeStock, 0, len(stocks))
	var sumChange float64
	var quoted int
	var positive int

	bench := quotes[market.QuoteMapKey(marketName, benchmarkCode)]
	benchChange := 0.0
	if bench.HasQuote {
		benchChange = bench.ChangePercent
	}

	for _, st := range stocks {
		q := quotes[market.QuoteMapKey(marketName, st.Symbol)]
		name := st.CompanyName
		if name == "" {
			name = q.StockName
		}
		ts := ThemeStock{
			Symbol:      st.Symbol,
			CompanyName: name,
			Market:      marketName,
			SectorTags:  st.SectorTags,
			HasQuote:    q.HasQuote,
		}
		if q.HasQuote {
			ts.Price = q.Price
			ts.ChangePercent = q.ChangePercent
			sumChange += q.ChangePercent
			quoted++
			if q.ChangePercent > 0 {
				positive++
			}
		}
		themeStocks = append(themeStocks, ts)
	}

	sort.Slice(themeStocks, func(i, j int) bool {
		return themeStocks[i].ChangePercent > themeStocks[j].ChangePercent
	})

	topN := 5
	if len(themeStocks) < topN {
		topN = len(themeStocks)
	}

	avgChange := 0.0
	breadth := 0.0
	if quoted > 0 {
		avgChange = sumChange / float64(quoted)
		breadth = float64(positive) / float64(quoted) * 100
	}

	return ThemeSnapshot{
		ID:                 def.ID,
		Name:               def.Name,
		Market:             marketName,
		State:              classifyState(avgChange, breadth),
		StockCount:         len(stocks),
		QuotedCount:        quoted,
		AvgChangePct:       round2(avgChange),
		BreadthPct:         round2(breadth),
		RsVsBenchmark:      round2(avgChange - benchChange),
		BenchmarkCode:      benchmarkCode,
		BenchmarkChangePct: round2(benchChange),
		TopGainers:         themeStocks[:topN],
	}
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}
