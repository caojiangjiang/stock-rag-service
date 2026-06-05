package theme

import (
	"context"
	"log"
	"strings"
)

// SymbolThemeMembership symbol 所属主题（market:SYMBOL → themes）。
type SymbolThemeMembership struct {
	ThemeID   string `json:"theme_id"`
	ThemeName string `json:"theme_name"`
	Market    string `json:"market"`
}

// SymbolIndex 返回 symbol→主题映射（Redis 缓存，复用 BoardLoader 板块缓存）。
func (s *Service) SymbolIndex(ctx context.Context) (map[string][]SymbolThemeMembership, error) {
	if s.symbolIndexStore != nil && s.symbolIndexStore.Enabled() {
		var index map[string][]SymbolThemeMembership
		_, ok, err := s.symbolIndexStore.Get(ctx, symbolIndexCacheKey, &index)
		if err != nil {
			log.Printf("theme symbol index redis get: %v", err)
		} else if ok && len(index) > 0 {
			return index, nil
		}
	}

	reg, err := s.loadRegistry(s.registryPath)
	if err != nil {
		return nil, err
	}
	universe, err := s.loadUniverse(s.universePath)
	if err != nil {
		return nil, err
	}

	index := make(map[string][]SymbolThemeMembership)
	for _, def := range reg.Themes {
		stocks, _ := ResolveStocks(ctx, s.boards, def, universe)
		for _, st := range stocks {
			key := symbolIndexKey(st.Market, st.Symbol)
			index[key] = append(index[key], SymbolThemeMembership{
				ThemeID:   def.ID,
				ThemeName: def.Name,
				Market:    strings.ToLower(st.Market),
			})
		}
	}

	if s.symbolIndexStore != nil && s.symbolIndexStore.Enabled() {
		if err := s.symbolIndexStore.Set(ctx, symbolIndexCacheKey, index, symbolIndexTTL()); err != nil {
			log.Printf("theme symbol index redis set: %v", err)
		}
	}
	return index, nil
}

func symbolIndexKey(market, symbol string) string {
	return strings.ToLower(strings.TrimSpace(market)) + ":" + strings.ToUpper(strings.TrimSpace(symbol))
}
