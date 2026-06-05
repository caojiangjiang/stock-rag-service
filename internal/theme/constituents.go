package theme

import (
	"context"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"stock_rag/internal/market"
	personamodel "stock_rag/internal/persona/model"
)

type boardCacheEntry struct {
	members []market.BoardMember
	at      time.Time
}

// BoardLoader 概念板块成分加载器。
type BoardLoader struct {
	mu    sync.Mutex
	cache map[string]boardCacheEntry
	ttl   time.Duration
}

func NewBoardLoader() *BoardLoader {
	return &BoardLoader{
		cache: make(map[string]boardCacheEntry),
		ttl:   time.Hour,
	}
}

func (l *BoardLoader) Members(ctx context.Context, def ThemeDef) ([]market.BoardMember, error) {
	boardCode := strings.TrimSpace(def.EastMoneyBoardCN)
	if boardCode == "" && strings.TrimSpace(def.EastMoneyBoardNameCN) != "" {
		code, _, err := market.ResolveConceptBoard(ctx, def.EastMoneyBoardNameCN)
		if err != nil {
			return nil, err
		}
		boardCode = code
	}
	if boardCode == "" {
		return nil, nil
	}

	l.mu.Lock()
	if hit, ok := l.cache[boardCode]; ok && time.Since(hit.at) < l.ttl {
		l.mu.Unlock()
		return hit.members, nil
	}
	l.mu.Unlock()

	maxN := def.MaxConstituents
	if maxN <= 0 {
		maxN = 40
	}
	members, err := market.FetchBoardConstituents(ctx, boardCode, maxN)
	if err != nil {
		return nil, err
	}

	l.mu.Lock()
	l.cache[boardCode] = boardCacheEntry{members: members, at: time.Now()}
	l.mu.Unlock()
	return members, nil
}

// ResolveStocks 合并东财概念成分 + 标签匹配 + 额外 symbol，并返回板块已带的行情。
func ResolveStocks(ctx context.Context, loader *BoardLoader, def ThemeDef, universe *personamodel.UniverseConfig) ([]personamodel.CandidateStock, map[string]market.Quote) {
	seen := make(map[string]personamodel.CandidateStock)
	quotes := make(map[string]market.Quote)
	add := func(stock personamodel.CandidateStock) {
		key := market.QuoteMapKey(stock.Market, stock.Symbol)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = stock
	}

	if loader != nil {
		members, err := loader.Members(ctx, def)
		if err != nil {
			log.Printf("theme %s board load: %v", def.ID, err)
		} else {
			for _, m := range members {
				if m.HasQuote {
					quotes[market.QuoteMapKey("cn", m.Symbol)] = market.Quote{
						StockCode:     strings.ToUpper(m.Symbol),
						StockName:     m.CompanyName,
						Price:         m.Price,
						ChangePercent: m.ChangePercent,
						HasQuote:      true,
						Source:        "eastmoney",
						AssetType:     "stock",
					}
				}
				found := findInUniverse(universe, "cn", m.Symbol)
				if found != nil {
					add(*found)
					continue
				}
				add(personamodel.CandidateStock{
					Symbol:      m.Symbol,
					CompanyName: m.CompanyName,
					Market:      "cn",
				})
			}
		}
	}

	for _, s := range MatchStocks(def, universe) {
		add(s)
	}

	out := make([]personamodel.CandidateStock, 0, len(seen))
	for _, s := range seen {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	return out, quotes
}
