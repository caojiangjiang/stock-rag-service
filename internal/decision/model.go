package decision

import (
	"time"

	"stock_rag/internal/portfolio"
)

// DailyBrief 每日决策简报（P0 核心交付）。
type DailyBrief struct {
	AsOf            time.Time         `json:"as_of"`
	Portfolio       PortfolioSection  `json:"portfolio"`
	ThemeExposures  []ThemeExposure   `json:"theme_exposures"`
	WatchItems      []WatchItem       `json:"watch_items"`
	Risk            RiskReport        `json:"risk"`
	PromptTemplates []PromptTemplate  `json:"prompt_templates"`
	DataTrust       DataTrustSummary  `json:"data_trust"`
	Cached          bool              `json:"cached,omitempty"`
	CacheAgeSec     int               `json:"cache_age_sec,omitempty"`
}

// PortfolioSection 持仓摘要（含数据来源）。
type PortfolioSection struct {
	Summary portfolio.Summary `json:"summary"`
}

// ThemeExposure 持仓在某主题上的暴露。
type ThemeExposure struct {
	ThemeID      string   `json:"theme_id"`
	ThemeName    string   `json:"theme_name"`
	Market       string   `json:"market"`
	State        string   `json:"state"`
	AvgChangePct float64  `json:"avg_change_pct"`
	WeightPct    float64  `json:"weight_pct"`
	Symbols      []string `json:"symbols"`
	Alert        string   `json:"alert,omitempty"`
}

// WatchItem 今日需关注事项（规则生成，最多 5 条）。
type WatchItem struct {
	Severity string   `json:"severity"` // info | warn | critical
	Title    string   `json:"title"`
	Detail   string   `json:"detail"`
	Symbols  []string `json:"symbols,omitempty"`
}

// RiskReport 风险规则检查结果（P1）。
type RiskReport struct {
	Passed     bool            `json:"passed"`
	Violations []RiskViolation `json:"violations"`
}

// RiskViolation 单条风险违规/警告。
type RiskViolation struct {
	RuleID   string  `json:"rule_id"`
	Severity string  `json:"severity"` // warn | block
	Message  string  `json:"message"`
	Symbol   string  `json:"symbol,omitempty"`
	ThemeID  string  `json:"theme_id,omitempty"`
	Value    float64 `json:"value,omitempty"`
	Limit    float64 `json:"limit,omitempty"`
}

// PromptTemplate 固定追问模板（P0）。
type PromptTemplate struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

// DataTrustSummary 数据可信度汇总（P0）。
type DataTrustSummary struct {
	QuoteLive       int  `json:"quote_live"`
	QuoteMock       int  `json:"quote_mock"`
	QuoteMissing    int  `json:"quote_missing"`
	ThemeCached     bool `json:"theme_cached"`
	ThemeCacheAgeSec int `json:"theme_cache_age_sec"`
	Note            string `json:"note,omitempty"`
}

// DailyRecord 每日快照记录（P1 跟踪）。
type DailyRecord struct {
	Date           string             `json:"date"`
	UserID         string             `json:"user_id"`
	TotalValue     float64            `json:"total_value"`
	TotalCost      float64            `json:"total_cost"`
	UnrealizedPct  float64            `json:"unrealized_pnl_pct"`
	ThemeSnapshots []ThemeRecord      `json:"theme_snapshots"`
	RecordedAt     time.Time          `json:"recorded_at"`
}

// ThemeRecord 主题状态记录。
type ThemeRecord struct {
	ThemeID      string  `json:"theme_id"`
	ThemeName    string  `json:"theme_name"`
	Market       string  `json:"market"`
	State        string  `json:"state"`
	AvgChangePct float64 `json:"avg_change_pct"`
	ExposurePct  float64 `json:"exposure_pct"`
}
