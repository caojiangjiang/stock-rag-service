package theme

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"stock_rag/internal/market"
	personamodel "stock_rag/internal/persona/model"
)

// Service 主题快照服务。
type Service struct {
	registryPath string
	universePath string
	market       market.Provider
	loadRegistry func(string) (*Registry, error)
	loadUniverse func(string) (*personamodel.UniverseConfig, error)
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
		loadRegistry: LoadRegistry,
		loadUniverse: LoadUniverseYAML,
	}
}

// Snapshot 返回主题快照列表。
func (s *Service) Snapshot(ctx context.Context, themeID, marketFilter string) (*SnapshotResponse, error) {
	_ = ctx
	reg, err := s.loadRegistry(s.registryPath)
	if err != nil {
		return nil, err
	}
	universe, err := s.loadUniverse(s.universePath)
	if err != nil {
		return nil, fmt.Errorf("load universe: %w", err)
	}

	now := time.Now()
	resp := &SnapshotResponse{AsOf: now}

	for _, def := range reg.Themes {
		if themeID != "" && !strings.EqualFold(def.ID, themeID) {
			continue
		}
		snaps := s.buildSnapshots(def, universe, marketFilter)
		resp.Themes = append(resp.Themes, snaps...)
	}
	if themeID != "" && len(resp.Themes) == 0 {
		return nil, fmt.Errorf("theme not found: %s", themeID)
	}
	return resp, nil
}

func (s *Service) buildSnapshots(def ThemeDef, universe *personamodel.UniverseConfig, marketFilter string) []ThemeSnapshot {
	stocks := MatchStocks(def, universe)
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
		out = append(out, s.aggregateTheme(def, m, list, bench))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Market < out[j].Market })
	return out
}

func (s *Service) aggregateTheme(def ThemeDef, marketName string, stocks []personamodel.CandidateStock, benchmarkCode string) ThemeSnapshot {
	themeStocks := make([]ThemeStock, 0, len(stocks))
	var sumChange float64
	var quoted int
	var positive int

	bench := s.market.GetQuote(benchmarkCode)
	benchChange := 0.0
	if bench.HasQuote {
		benchChange = bench.ChangePercent
	}

	for _, st := range stocks {
		q := s.market.GetQuote(st.Symbol)
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
