package theme

import (
	"fmt"
	"os"
	"sort"
	"strings"

	personamodel "stock_rag/internal/persona/model"

	"gopkg.in/yaml.v3"
)

// UniverseLoader 加载候选池。
type UniverseLoader func(path string) (*personamodel.UniverseConfig, error)

// LoadRegistry 从 YAML 加载主题注册表。
func LoadRegistry(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read theme registry: %w", err)
	}
	var reg Registry
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse theme registry: %w", err)
	}
	return &reg, nil
}

// LoadUniverseYAML 加载 persona 候选池配置。
func LoadUniverseYAML(path string) (*personamodel.UniverseConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg personamodel.UniverseConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// MatchStocks 按主题定义匹配 universe 成分。
func MatchStocks(def ThemeDef, universe *personamodel.UniverseConfig) []personamodel.CandidateStock {
	if universe == nil {
		return nil
	}

	tagSet := make(map[string]struct{}, len(def.MatchTags))
	for _, t := range def.MatchTags {
		tagSet[strings.ToLower(strings.TrimSpace(t))] = struct{}{}
	}

	seen := make(map[string]personamodel.CandidateStock)
	add := func(stock personamodel.CandidateStock) {
		key := strings.ToUpper(stock.Market) + ":" + strings.ToUpper(stock.Symbol)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = stock
	}

	matchTags := func(stock personamodel.CandidateStock) bool {
		for _, tag := range stock.SectorTags {
			if _, ok := tagSet[strings.ToLower(strings.TrimSpace(tag))]; ok {
				return true
			}
		}
		return false
	}

	for _, s := range universe.US {
		s.Market = "us"
		if matchTags(s) {
			add(s)
		}
	}
	for _, s := range universe.CN {
		s.Market = "cn"
		if matchTags(s) {
			add(s)
		}
	}

	for market, symbols := range def.ExtraSymbols {
		m := strings.ToLower(strings.TrimSpace(market))
		for _, sym := range symbols {
			sym = strings.TrimSpace(sym)
			if sym == "" {
				continue
			}
			found := findInUniverse(universe, m, sym)
			if found != nil {
				add(*found)
				continue
			}
			add(personamodel.CandidateStock{
				Symbol:      sym,
				CompanyName: sym,
				Market:      m,
			})
		}
	}

	out := make([]personamodel.CandidateStock, 0, len(seen))
	for _, s := range seen {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Symbol < out[j].Symbol
	})
	return out
}

func findInUniverse(universe *personamodel.UniverseConfig, market, symbol string) *personamodel.CandidateStock {
	symbol = strings.ToUpper(symbol)
	if market == "us" {
		for i := range universe.US {
			if strings.EqualFold(universe.US[i].Symbol, symbol) {
				s := universe.US[i]
				s.Market = "us"
				return &s
			}
		}
	}
	for i := range universe.CN {
		if universe.CN[i].Symbol == symbol {
			s := universe.CN[i]
			s.Market = "cn"
			return &s
		}
	}
	return nil
}

func classifyState(avgChange, breadth float64) string {
	switch {
	case avgChange > 5 && breadth >= 80:
		return "overheated"
	case avgChange > 2 && breadth >= 60:
		return "hot"
	case avgChange > 0 && breadth >= 45:
		return "emerging"
	case avgChange < -1:
		return "cooling"
	default:
		return "neutral"
	}
}
