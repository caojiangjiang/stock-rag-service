// internal/repository/user_memory.go
package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"stock_rag/internal/embedding"
	"stock_rag/internal/vectorstore"

	"github.com/jackc/pgx/v4/pgxpool"
)

// UserMemory 用户长期记忆
type UserMemory struct {
	UserID      string           `json:"user_id"`
	Preferences *UserPreferences `json:"preferences"` // 用户偏好
	Insights    []*Insight       `json:"insights"`    // 历史见解
	StockPool   []string         `json:"stock_pool"`  // 关注的股票池
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

// UserPreferences 用户偏好
type UserPreferences struct {
	InterestedStocks []string     `json:"interested_stocks"` // 感兴趣的股票
	PreferredMetrics []string     `json:"preferred_metrics"` // 偏好的指标：如 ["pe_ratio", "cash_flow"]
	DetailLevel      DetailLevel  `json:"detail_level"`      // 喜欢详细数据 vs 简洁结论
	RiskAppetite     RiskAppetite `json:"risk_appetite"`     // 风险偏好：high/medium/low
	InteractionStyle string       `json:"interaction_style"` // 交互风格：如 "专业型"、"入门型"
}

// DetailLevel 详细程度枚举
type DetailLevel string

const (
	DetailLevelBrief  DetailLevel = "brief"  // 简洁结论
	DetailLevelNormal DetailLevel = "normal" // 正常
	DetailLevelDetail DetailLevel = "detail" // 详细数据
)

// RiskAppetite 风险偏好枚举
type RiskAppetite string

const (
	RiskAppetiteHigh   RiskAppetite = "high"
	RiskAppetiteMedium RiskAppetite = "medium"
	RiskAppetiteLow    RiskAppetite = "low"
)

// Insight 历史见解
type Insight struct {
	InsightID      string    `json:"insight_id"`       // 见解 ID
	ConversationID string    `json:"conversation_id"`  // 所属会话
	UserID         string    `json:"user_id"`          // 用户 ID
	Content        string    `json:"content"`          // 见解内容
	Summary        string    `json:"summary"`          // 简要摘要
	Entities       []string  `json:"entities"`         // 关联实体：如 ["贵州茅台", "五粮液"]
	Vector         []float32 `json:"vector"`           // 向量（用于语义检索）
	CreatedAt      time.Time `json:"created_at"`       // 创建时间
	LastRecalledAt time.Time `json:"last_recalled_at"` // 最后召回时间
	RecallCount    int       `json:"recall_count"`     // 召回次数
}

// UserMemoryStore 长期记忆存储接口
type UserMemoryStore interface {
	// Get 获取用户记忆
	Get(ctx context.Context, userID string) (*UserMemory, error)

	// Save 保存用户记忆
	Save(ctx context.Context, memory *UserMemory) error

	// UpdatePreferences 更新用户偏好
	UpdatePreferences(ctx context.Context, userID string, prefs *UserPreferences) error

	// AddInsight 添加历史见解
	AddInsight(ctx context.Context, userID string, insight *Insight) error

	// SearchInsights 检索相关见解（语义检索）
	SearchInsights(ctx context.Context, userID string, query string, limit int) ([]*Insight, error)

	// RecallInsight 召回见解（增加召回计数）
	RecallInsight(ctx context.Context, insightID string) error

	// UpdateStockPool 更新关注股票池
	UpdateStockPool(ctx context.Context, userID string, stocks []string) error

	// ExtractAndUpdatePreferences 从对话中提取偏好并更新
	ExtractAndUpdatePreferences(ctx context.Context, userID string, messages []*Message) error
}

// PostgresUserMemoryStore 基于 PostgreSQL + Vector DB 的长期记忆存储
type PostgresUserMemoryStore struct {
	db          *pgxpool.Pool
	vectorStore vectorstore.VectorStore
	embedder    embedding.Embedder
}

// NewPostgresUserMemoryStore 创建长期记忆存储
func NewPostgresUserMemoryStore(db *pgxpool.Pool, vectorStore vectorstore.VectorStore, embedder embedding.Embedder) *PostgresUserMemoryStore {
	return &PostgresUserMemoryStore{
		db:          db,
		vectorStore: vectorStore,
		embedder:    embedder,
	}
}

// UserMemoryTable 表名
const UserMemoryTable = "user_memories"

// InsightTable 表名
const InsightTable = "insights"

// InitTables 初始化表结构
func (s *PostgresUserMemoryStore) InitTables(ctx context.Context) error {
	// 创建用户记忆表
	_, err := s.db.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			user_id TEXT PRIMARY KEY,
			preferences JSONB,
			stock_pool TEXT[],
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL
		)
	`, UserMemoryTable))
	if err != nil {
		return fmt.Errorf("failed to create user_memories table: %w", err)
	}

	// 创建见解表
	_, err = s.db.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			insight_id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			conversation_id TEXT NOT NULL,
			content TEXT NOT NULL,
			summary TEXT,
			entities TEXT[],
			vector_id TEXT,
			created_at BIGINT NOT NULL,
			last_recalled_at BIGINT,
			recall_count INT DEFAULT 0
		)
	`, InsightTable))
	if err != nil {
		return fmt.Errorf("failed to create insights table: %w", err)
	}

	// 创建索引
	_, err = s.db.Exec(ctx, fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_insights_user_id ON %s (user_id)
	`, InsightTable))
	if err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	_, err = s.db.Exec(ctx, fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_insights_created_at ON %s (created_at)
	`, InsightTable))
	if err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	return nil
}

// Get 获取用户记忆
func (s *PostgresUserMemoryStore) Get(ctx context.Context, userID string) (*UserMemory, error) {
	// 查询用户偏好
	var preferencesJSON []byte
	var stockPool []string
	var createdAt, updatedAt int64

	err := s.db.QueryRow(ctx, fmt.Sprintf(`
		SELECT preferences, stock_pool, created_at, updated_at FROM %s WHERE user_id = $1
	`, UserMemoryTable), userID).Scan(&preferencesJSON, &stockPool, &createdAt, &updatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return nil, ErrNotFound
		}
		return nil, err
	}

	memory := &UserMemory{
		UserID:    userID,
		StockPool: stockPool,
		CreatedAt: time.Unix(createdAt, 0),
		UpdatedAt: time.Unix(updatedAt, 0),
		Insights:  []*Insight{},
	}

	if preferencesJSON != nil {
		var prefs UserPreferences
		if err := json.Unmarshal(preferencesJSON, &prefs); err == nil {
			memory.Preferences = &prefs
		}
	}

	// 查询见解
	insights, err := s.getInsights(ctx, userID)
	if err == nil {
		memory.Insights = insights
	}

	return memory, nil
}

// getInsights 获取用户的所有见解
func (s *PostgresUserMemoryStore) getInsights(ctx context.Context, userID string) ([]*Insight, error) {
	rows, err := s.db.Query(ctx, fmt.Sprintf(`
		SELECT insight_id, conversation_id, content, summary, entities, created_at, last_recalled_at, recall_count
		FROM %s
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, InsightTable), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var insights []*Insight
	for rows.Next() {
		var insight Insight
		var summary *string
		var entities []string
		var lastRecalledAt *int64

		err := rows.Scan(
			&insight.InsightID,
			&insight.ConversationID,
			&insight.Content,
			&summary,
			&entities,
			&insight.CreatedAt,
			&lastRecalledAt,
			&insight.RecallCount,
		)
		if err != nil {
			return nil, err
		}

		if summary != nil {
			insight.Summary = *summary
		}
		insight.Entities = entities
		if lastRecalledAt != nil {
			insight.LastRecalledAt = time.Unix(*lastRecalledAt, 0)
		}

		insights = append(insights, &insight)
	}

	return insights, nil
}

// Save 保存用户记忆
func (s *PostgresUserMemoryStore) Save(ctx context.Context, memory *UserMemory) error {
	now := time.Now().Unix()

	if memory.CreatedAt.IsZero() {
		memory.CreatedAt = time.Now()
	}
	memory.UpdatedAt = time.Now()

	preferencesJSON, err := json.Marshal(memory.Preferences)
	if err != nil {
		return fmt.Errorf("failed to marshal preferences: %w", err)
	}

	_, err = s.db.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (user_id, preferences, stock_pool, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id) DO UPDATE SET
			preferences = $2,
			stock_pool = $3,
			updated_at = $5
	`, UserMemoryTable),
		memory.UserID,
		preferencesJSON,
		memory.StockPool,
		memory.CreatedAt.Unix(),
		now)

	return err
}

// UpdatePreferences 更新用户偏好
func (s *PostgresUserMemoryStore) UpdatePreferences(ctx context.Context, userID string, prefs *UserPreferences) error {
	memory, err := s.Get(ctx, userID)
	if err != nil && err != ErrNotFound {
		return err
	}

	if memory == nil {
		memory = &UserMemory{
			UserID:      userID,
			Preferences: prefs,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
	} else {
		memory.Preferences = prefs
		memory.UpdatedAt = time.Now()
	}

	return s.Save(ctx, memory)
}

// AddInsight 添加历史见解
func (s *PostgresUserMemoryStore) AddInsight(ctx context.Context, userID string, insight *Insight) error {
	if insight.InsightID == "" {
		insight.InsightID = fmt.Sprintf("insight-%d", time.Now().UnixNano())
	}
	insight.UserID = userID
	insight.CreatedAt = time.Now()

	// 如果有 embedder，生成向量
	if s.embedder != nil && insight.Content != "" {
		vector, err := s.embedder.EmbedDocuments(ctx, []string{insight.Content})
		if err == nil && len(vector) > 0 {
			insight.Vector = vector[0]

			// 同时存入向量数据库
			if s.vectorStore != nil {
				record := vectorstore.Record{
					ID:      insight.InsightID,
					Content: insight.Content,
					Vector:  insight.Vector,
					Metadata: map[string]string{
						"user_id":         userID,
						"conversation_id": insight.ConversationID,
						"entities":        strings.Join(insight.Entities, ","),
					},
				}
				if err := s.vectorStore.Upsert(ctx, []vectorstore.Record{record}); err != nil {
					// 向量存储失败不影响主存储
				}
			}
		}
	}

	entities := insight.Entities
	if entities == nil {
		entities = []string{}
	}

	_, err := s.db.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (insight_id, user_id, conversation_id, content, summary, entities, created_at, recall_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (insight_id) DO UPDATE SET
			content = $4,
			summary = $5,
			entities = $6
	`, InsightTable),
		insight.InsightID,
		insight.UserID,
		insight.ConversationID,
		insight.Content,
		insight.Summary,
		entities,
		insight.CreatedAt.Unix(),
		0)

	return err
}

// SearchInsights 检索相关见解
func (s *PostgresUserMemoryStore) SearchInsights(ctx context.Context, userID string, query string, limit int) ([]*Insight, error) {
	if limit <= 0 {
		limit = 5
	}

	// 如果有向量存储，使用向量检索
	if s.vectorStore != nil && s.embedder != nil {
		vector, err := s.embedder.EmbedQuery(ctx, query)
		if err == nil && len(vector) > 0 {
			results, err := s.vectorStore.Search(ctx, vectorstore.SearchRequest{
				QueryText:   query,
				QueryVector: vector,
				TopK:        limit,
				Filter:      vectorstore.Filter{},
			})
			if err == nil && len(results) > 0 {
				return s.buildInsightsFromSearchResults(ctx, userID, results)
			}
		}
	}

	// fallback 到数据库全文检索
	return s.searchInsightsFromDB(ctx, userID, query, limit)
}

// buildInsightsFromSearchResults 从向量检索结果构建见解
func (s *PostgresUserMemoryStore) buildInsightsFromSearchResults(ctx context.Context, userID string, results []vectorstore.SearchResult) ([]*Insight, error) {
	var insights []*Insight

	for _, result := range results {
		metadata := result.Citation.DocType // 复用 DocType 存 entities
		entities := strings.Split(metadata, ",")

		insight := &Insight{
			Content:  result.Content,
			Summary:  result.Content[:min(100, len(result.Content))],
			Entities: entities,
		}

		insights = append(insights, insight)
	}

	return insights, nil
}

// searchInsightsFromDB 从数据库检索见解
func (s *PostgresUserMemoryStore) searchInsightsFromDB(ctx context.Context, userID string, query string, limit int) ([]*Insight, error) {
	rows, err := s.db.Query(ctx, fmt.Sprintf(`
		SELECT insight_id, conversation_id, content, summary, entities, created_at, last_recalled_at, recall_count
		FROM %s
		WHERE user_id = $1 AND (content LIKE '%%' || $2 || '%%' OR summary LIKE '%%' || $2 || '%%')
		ORDER BY recall_count DESC, created_at DESC
		LIMIT $3
	`, InsightTable), userID, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var insights []*Insight
	for rows.Next() {
		var insight Insight
		var summary *string
		var entities []string
		var lastRecalledAt *int64

		err := rows.Scan(
			&insight.InsightID,
			&insight.ConversationID,
			&insight.Content,
			&summary,
			&entities,
			&insight.CreatedAt,
			&lastRecalledAt,
			&insight.RecallCount,
		)
		if err != nil {
			return nil, err
		}

		if summary != nil {
			insight.Summary = *summary
		}
		insight.Entities = entities
		if lastRecalledAt != nil {
			insight.LastRecalledAt = time.Unix(*lastRecalledAt, 0)
		}

		insights = append(insights, &insight)
	}

	return insights, nil
}

// RecallInsight 召回见解
func (s *PostgresUserMemoryStore) RecallInsight(ctx context.Context, insightID string) error {
	_, err := s.db.Exec(ctx, fmt.Sprintf(`
		UPDATE %s
		SET recall_count = recall_count + 1, last_recalled_at = $1
		WHERE insight_id = $2
	`, InsightTable), time.Now().Unix(), insightID)
	return err
}

// UpdateStockPool 更新关注股票池
func (s *PostgresUserMemoryStore) UpdateStockPool(ctx context.Context, userID string, stocks []string) error {
	memory, err := s.Get(ctx, userID)
	if err != nil && err != ErrNotFound {
		return err
	}

	if memory == nil {
		memory = &UserMemory{
			UserID:    userID,
			StockPool: stocks,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	} else {
		// 合并股票池
		existing := make(map[string]bool)
		for _, s := range memory.StockPool {
			existing[s] = true
		}
		for _, s := range stocks {
			existing[s] = true
		}

		var merged []string
		for s := range existing {
			merged = append(merged, s)
		}
		memory.StockPool = merged
		memory.UpdatedAt = time.Now()
	}

	return s.Save(ctx, memory)
}

// ExtractAndUpdatePreferences 从对话中提取偏好并更新
func (s *PostgresUserMemoryStore) ExtractAndUpdatePreferences(ctx context.Context, userID string, messages []*Message) error {
	if len(messages) == 0 {
		return nil
	}

	// 简单实现：统计消息中的股票提及次数
	stockMentions := make(map[string]int)
	var totalMessages int

	for _, msg := range messages {
		if msg.Metadata != nil {
			if stockCode, ok := msg.Metadata["stock_code"]; ok {
				stockMentions[fmt.Sprintf("%v", stockCode)]++
			}
		}
		totalMessages++
	}

	// 获取现有偏好
	memory, err := s.Get(ctx, userID)
	if err != nil && err != ErrNotFound {
		return err
	}

	var prefs *UserPreferences
	if memory != nil && memory.Preferences != nil {
		prefs = memory.Preferences
	} else {
		prefs = &UserPreferences{}
	}

	// 更新关注的股票（只保留提及次数 >= 2 的）
	var stocks []string
	for stock, count := range stockMentions {
		if count >= 2 {
			stocks = append(stocks, stock)
		}
	}

	if len(stocks) > 0 {
		if prefs.InterestedStocks == nil {
			prefs.InterestedStocks = []string{}
		}
		prefs.InterestedStocks = append(prefs.InterestedStocks, stocks...)
	}

	return s.UpdatePreferences(ctx, userID, prefs)
}

// AddInterestedStock 添加关注的股票
func (s *PostgresUserMemoryStore) AddInterestedStock(ctx context.Context, userID string, stock string) error {
	memory, err := s.Get(ctx, userID)
	if err != nil && err != ErrNotFound {
		return err
	}

	if memory == nil {
		memory = &UserMemory{
			UserID:    userID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}

	if memory.Preferences == nil {
		memory.Preferences = &UserPreferences{}
	}

	if memory.Preferences.InterestedStocks == nil {
		memory.Preferences.InterestedStocks = []string{}
	}

	// 检查是否已存在
	for _, s := range memory.Preferences.InterestedStocks {
		if s == stock {
			return nil // 已存在
		}
	}

	memory.Preferences.InterestedStocks = append(memory.Preferences.InterestedStocks, stock)
	memory.UpdatedAt = time.Now()

	return s.Save(ctx, memory)
}

// AddPreferredMetric 添加偏好的指标
func (s *PostgresUserMemoryStore) AddPreferredMetric(ctx context.Context, userID string, metric string) error {
	memory, err := s.Get(ctx, userID)
	if err != nil && err != ErrNotFound {
		return err
	}

	if memory == nil {
		memory = &UserMemory{
			UserID:    userID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}

	if memory.Preferences == nil {
		memory.Preferences = &UserPreferences{}
	}

	if memory.Preferences.PreferredMetrics == nil {
		memory.Preferences.PreferredMetrics = []string{}
	}

	// 检查是否已存在
	for _, m := range memory.Preferences.PreferredMetrics {
		if m == metric {
			return nil // 已存在
		}
	}

	memory.Preferences.PreferredMetrics = append(memory.Preferences.PreferredMetrics, metric)
	memory.UpdatedAt = time.Now()

	return s.Save(ctx, memory)
}

// GetUserPreferences 获取用户偏好
func (s *PostgresUserMemoryStore) GetUserPreferences(ctx context.Context, userID string) (*UserPreferences, error) {
	memory, err := s.Get(ctx, userID)
	if err != nil {
		return nil, err
	}

	if memory == nil {
		return nil, nil
	}

	return memory.Preferences, nil
}
