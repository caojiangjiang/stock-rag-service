package model

type PersonaCard struct {
	PersonaID                string                    `json:"persona_id"`
	Name                     string                    `json:"name"`
	Market                   string                    `json:"market"`
	StyleTags                []string                  `json:"style_tags"`
	OneLiner                 string                    `json:"one_liner"`
	RiskLevel                string                    `json:"risk_level"`
	Performance              PersonaPerformanceSummary `json:"performance"`
	PreferredSectors         []string                  `json:"preferred_sectors"`
	SuitableMarketConditions []string                  `json:"suitable_market_conditions"`
	AvatarKey                string                    `json:"avatar_key"`
	Status                   string                    `json:"status"`
}

type PersonaPerformanceSummary struct {
	Return1Y      float64 `yaml:"return_1y" json:"return_1y"`
	MaxDrawdown1Y float64 `yaml:"max_drawdown_1y" json:"max_drawdown_1y"`
	Sharpe1Y      float64 `yaml:"sharpe_1y" json:"sharpe_1y"`
	TurnoverRate  float64 `yaml:"turnover_rate" json:"turnover_rate"`
	Concentration float64 `yaml:"concentration" json:"concentration"`
}

type PersonaProfile struct {
	PersonaID                  string                    `yaml:"persona_id" json:"persona_id"`
	Name                       string                    `yaml:"name" json:"name"`
	Market                     string                    `yaml:"market" json:"market"`
	StyleTags                  []string                  `yaml:"style_tags" json:"style_tags"`
	OneLiner                   string                    `yaml:"one_liner" json:"one_liner"`
	InvestmentBelief           string                    `yaml:"investment_belief" json:"investment_belief"`
	DecisionFramework          []string                  `yaml:"decision_framework" json:"decision_framework"`
	PreferredSectors           []string                  `yaml:"preferred_sectors" json:"preferred_sectors"`
	AvoidedSectors             []string                  `yaml:"avoided_sectors" json:"avoided_sectors"`
	SuitableMarketConditions   []string                  `yaml:"suitable_market_conditions" json:"suitable_market_conditions"`
	UnsuitableMarketConditions []string                  `yaml:"unsuitable_market_conditions" json:"unsuitable_market_conditions"`
	TypicalRisks               []string                  `yaml:"typical_risks" json:"typical_risks"`
	RepresentativeQuestions    []string                  `yaml:"representative_questions" json:"representative_questions"`
	Performance                PersonaPerformanceSummary `yaml:"performance" json:"performance"`
	DisagreePersonaIDs         []string                  `yaml:"disagree_persona_ids" json:"disagree_persona_ids"`
	AvatarKey                  string                    `yaml:"avatar_key" json:"avatar_key"`
	Status                     string                    `yaml:"status" json:"status"`
}

type EvidenceItem struct {
	CitationID  string  `json:"citation_id"`
	Title       string  `json:"title"`
	DocType     string  `json:"doc_type"`
	Source      string  `json:"source"`
	SourceURL   string  `json:"source_url"`
	PublishedAt string  `json:"published_at"`
	Snippet     string  `json:"snippet"`
	StockCode   string  `json:"stock_code"`
	Score       float64 `json:"score"`
}

type CounterView struct {
	PersonaID   string `json:"persona_id"`
	PersonaName string `json:"persona_name"`
	Summary     string `json:"summary"`
}

type PersonaAnswer struct {
	PersonaID   string         `json:"persona_id"`
	PersonaName string         `json:"persona_name"`
	Summary     string         `json:"summary,omitempty"`
	Stance      string         `json:"stance"`
	Thesis      []string       `json:"thesis"`
	Risks       []string       `json:"risks"`
	CounterView CounterView    `json:"counter_view"`
	Citations   []EvidenceItem `json:"citations"`
	Disclaimer  string         `json:"disclaimer"`
	RequestID   string         `json:"request_id"`
}

type RoundtableResponse struct {
	Question         string          `json:"question"`
	Participants     []PersonaCard   `json:"participants"`
	Answers          []PersonaAnswer `json:"answers"`
	Consensus        []string        `json:"consensus"`
	Disagreements    []string        `json:"disagreements"`
	RiskFocus        []string        `json:"risk_focus"`
	ModerationNotice string          `json:"moderation_notice"`
	RequestID        string          `json:"request_id"`
}

type PersonaChatRequest struct {
	PersonaID string `json:"persona_id"`
	Message   string `json:"message"`
	StockCode string `json:"stock_code,omitempty"`
	TimeRange string `json:"time_range,omitempty"`
	// UseRAG 为 true 时走 RAG 检索+生成；为 false 时仅按角色设定直答（无引用来源）
	// 未传时由服务端环境变量 PERSONA_CHAT_USE_RAG 决定，默认 true
	UseRAG *bool `json:"use_rag,omitempty"`
	// ConversationID 预留字段，用于多轮对话历史管理
	// 当前版本未实现，如需多轮对话请自行拼接历史消息
	ConversationID string `json:"conversation_id,omitempty"`
	CitationsLimit int    `json:"citations_limit,omitempty"`
}

type RoundtableRequest struct {
	Question                 string   `json:"question"`
	PersonaIDs               []string `json:"persona_ids,omitempty"`
	TimeRange                string   `json:"time_range,omitempty"`
	CitationsLimitPerPersona int      `json:"citations_limit_per_persona,omitempty"`
}
