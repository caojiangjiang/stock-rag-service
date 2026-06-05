package theme

import "time"

// Registry 主题配置根。
type Registry struct {
	Themes []ThemeDef `yaml:"themes"`
}

// ThemeDef 单个主题定义。
type ThemeDef struct {
	ID                   string            `yaml:"id"`
	Name                 string            `yaml:"name"`
	MatchTags            []string          `yaml:"match_tags"`
	ExtraSymbols         map[string][]string `yaml:"extra_symbols"`
	EastMoneyBoardCN     string            `yaml:"eastmoney_board_cn"`
	EastMoneyBoardNameCN string            `yaml:"eastmoney_board_name_cn"`
	MaxConstituents      int               `yaml:"max_constituents"`
	BenchmarkCN          string            `yaml:"benchmark_cn"`
	BenchmarkUS          string            `yaml:"benchmark_us"`
}

// SnapshotResponse 主题快照 API 响应。
type SnapshotResponse struct {
	AsOf        time.Time       `json:"as_of"`
	Themes      []ThemeSnapshot `json:"themes"`
	Cached      bool            `json:"cached,omitempty"`
	CacheAgeSec int             `json:"cache_age_sec,omitempty"`
}

// ThemeSnapshot 单个主题快照。
type ThemeSnapshot struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Market         string       `json:"market"`
	State          string       `json:"state"`
	StockCount     int          `json:"stock_count"`
	QuotedCount    int          `json:"quoted_count"`
	AvgChangePct   float64      `json:"avg_change_pct"`
	BreadthPct     float64      `json:"breadth_pct"`
	RsVsBenchmark  float64      `json:"rs_vs_benchmark"`
	BenchmarkCode  string       `json:"benchmark_code"`
	BenchmarkChangePct float64  `json:"benchmark_change_pct"`
	TopGainers     []ThemeStock `json:"top_gainers"`
}

// ThemeStock 主题成分股快照。
type ThemeStock struct {
	Symbol        string   `json:"symbol"`
	CompanyName   string   `json:"company_name"`
	Market        string   `json:"market"`
	ChangePercent float64  `json:"change_percent"`
	Price         float64  `json:"price"`
	SectorTags    []string `json:"sector_tags,omitempty"`
	HasQuote      bool     `json:"has_quote"`
}
