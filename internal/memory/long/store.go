package long

import (
	"context"
	"errors"
	"time"

	"stock_rag/internal/repository"
)

// ErrNotFound is returned when user memory is not found.
var ErrNotFound = errors.New("long-term memory not found")

// UserMemoryTable is the PostgreSQL table for user preferences.
const UserMemoryTable = "user_memories"

// InsightTable is the PostgreSQL table for user insights.
const InsightTable = "insights"

// UserMemory holds long-term user preferences and insights.
type UserMemory struct {
	UserID      string           `json:"user_id"`
	Preferences *UserPreferences `json:"preferences"`
	Insights    []*Insight       `json:"insights"`
	StockPool   []string         `json:"stock_pool"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

// UserPreferences captures stable user preferences.
type UserPreferences struct {
	InterestedStocks []string     `json:"interested_stocks"`
	PreferredMetrics []string     `json:"preferred_metrics"`
	DetailLevel      DetailLevel  `json:"detail_level"`
	RiskAppetite     RiskAppetite `json:"risk_appetite"`
	InteractionStyle string       `json:"interaction_style"`
}

// DetailLevel controls response verbosity.
type DetailLevel string

const (
	DetailLevelBrief  DetailLevel = "brief"
	DetailLevelNormal DetailLevel = "normal"
	DetailLevelDetail DetailLevel = "detail"
)

// RiskAppetite is the user's risk tolerance.
type RiskAppetite string

const (
	RiskAppetiteHigh   RiskAppetite = "high"
	RiskAppetiteMedium RiskAppetite = "medium"
	RiskAppetiteLow    RiskAppetite = "low"
)

// Insight is a durable user-level insight with optional vector index.
type Insight struct {
	InsightID      string    `json:"insight_id"`
	ConversationID string    `json:"conversation_id"`
	UserID         string    `json:"user_id"`
	Content        string    `json:"content"`
	Summary        string    `json:"summary"`
	Entities       []string  `json:"entities"`
	Vector         []float32 `json:"vector"`
	CreatedAt      time.Time `json:"created_at"`
	LastRecalledAt time.Time `json:"last_recalled_at"`
	RecallCount    int       `json:"recall_count"`
}

// Store is the long-term user memory interface.
type Store interface {
	Get(ctx context.Context, userID string) (*UserMemory, error)
	Save(ctx context.Context, memory *UserMemory) error
	UpdatePreferences(ctx context.Context, userID string, prefs *UserPreferences) error
	AddInsight(ctx context.Context, userID string, insight *Insight) error
	// BatchAddInsights 批量添加洞察，用于会话结束时的记忆沉淀
	BatchAddInsights(ctx context.Context, userID string, insights []*Insight) error
	SearchInsights(ctx context.Context, userID string, query string, limit int) ([]*Insight, error)
	RecallInsight(ctx context.Context, insightID string) error
	UpdateStockPool(ctx context.Context, userID string, stocks []string) error
	ExtractAndUpdatePreferences(ctx context.Context, userID string, messages []*repository.Message) error
	InitSchema(ctx context.Context) error
}
