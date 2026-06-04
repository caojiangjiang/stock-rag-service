package model

import "time"

// DailyPicksRequest 每日选股请求
type DailyPicksRequest struct {
	PersonaID string `json:"persona_id,omitempty"`
	Date     string `json:"date,omitempty"`
}

// DailyPicksResponse 每日选股响应
type DailyPicksResponse struct {
	Persona      PersonaInfo     `json:"persona"`
	Picks        []StockPick     `json:"picks"`
	GeneratedAt  time.Time       `json:"generated_at"`
	Disclaimer   string          `json:"disclaimer"`
	Market       string          `json:"market"`
	TotalCount   int             `json:"total_count"`
}

// PersonaInfo Persona 基本信息
type PersonaInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Market      string   `json:"market"`
	StyleTags   []string `json:"style_tags"`
	Description string   `json:"description"`
}

// StockPick 单只股票选股结果
type StockPick struct {
	Rank            int           `json:"rank"`
	Symbol          string        `json:"symbol"`
	CompanyName     string        `json:"company_name"`
	Market          string        `json:"market"`
	Score           float64       `json:"score"`
	InclusionReason []string      `json:"inclusion_reason"`
	RiskTips        []string      `json:"risk_tips"`
	EvidenceSummary *EvidenceRef  `json:"evidence_summary,omitempty"`
	SectorTags      []string      `json:"sector_tags"`
}

// EvidenceRef 证据引用
type EvidenceRef struct {
	TotalCount   int      `json:"total_count"`
	RecentCount  int      `json:"recent_count"`  // 近7天
	Sources      []string `json:"sources"`
	Summary      string   `json:"summary"`
}

// DailyPicksCache 每日选股缓存
type DailyPicksCache struct {
	Date     string                    `json:"date"`
	Picks    map[string]*DailyPicks   `json:"picks"` // key: persona_id
	UpdatedAt time.Time                `json:"updated_at"`
}

// DailyPicks 单个 Persona 的每日选股结果
type DailyPicks struct {
	PersonaID   string       `json:"persona_id"`
	PersonaName string       `json:"persona_name"`
	Market      string       `json:"market"`
	Date        string       `json:"date"`
	GeneratedAt time.Time    `json:"generated_at"`
	Stocks      []StockPick  `json:"stocks"`
	Status      string       `json:"status"` // success, partial, failed
	Error       string       `json:"error,omitempty"`
}

// RunResult 手动触发执行结果
type RunResult struct {
	TriggeredAt   time.Time `json:"triggered_at"`
	Status        string    `json:"status"`
	SuccessCount  int       `json:"success_count"`
	FailedCount   int       `json:"failed_count"`
	FailedPersonas []string `json:"failed_personas,omitempty"`
	Message       string    `json:"message,omitempty"`
}

// CandidateStock 候选股票
type CandidateStock struct {
	Symbol       string   `json:"symbol"`
	CompanyName  string   `json:"company_name"`
	Market       string   `json:"market"`
	SectorTags   []string `json:"sector_tags"`
}

// UniverseConfig 候选池配置
type UniverseConfig struct {
	US []CandidateStock `json:"us"`
	CN []CandidateStock `json:"cn"`
}
