package long

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"stock_rag/internal/embedding"
	"stock_rag/internal/repository"
	"stock_rag/internal/vectorstore"

	"github.com/jackc/pgx/v4/pgxpool"
)

// PostgresVectorStore implements Store using PostgreSQL and a vector index.
type PostgresVectorStore struct {
	db          *pgxpool.Pool
	vectorStore vectorstore.VectorStore
	embedder    embedding.Embedder
}

// NewPostgresVectorStore creates a long-term memory store.
func NewPostgresVectorStore(db *pgxpool.Pool, vectorStore vectorstore.VectorStore, embedder embedding.Embedder) *PostgresVectorStore {
	return &PostgresVectorStore{
		db:          db,
		vectorStore: vectorStore,
		embedder:    embedder,
	}
}

// InitSchema creates user_memories and insights tables.
func (s *PostgresVectorStore) InitSchema(ctx context.Context) error {
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
		return fmt.Errorf("create user_memories table: %w", err)
	}
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
		return fmt.Errorf("create insights table: %w", err)
	}
	_, err = s.db.Exec(ctx, fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_insights_user_id ON %s (user_id)
	`, InsightTable))
	if err != nil {
		return fmt.Errorf("create user_id index: %w", err)
	}
	_, err = s.db.Exec(ctx, fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_insights_created_at ON %s (created_at)
	`, InsightTable))
	if err != nil {
		return fmt.Errorf("create created_at index: %w", err)
	}
	return nil
}

func (s *PostgresVectorStore) Get(ctx context.Context, userID string) (*UserMemory, error) {
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
	insights, err := s.getInsights(ctx, userID)
	if err == nil {
		memory.Insights = insights
	}
	return memory, nil
}

func (s *PostgresVectorStore) getInsights(ctx context.Context, userID string) ([]*Insight, error) {
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
		var createdAt int64
		var lastRecalledAt *int64
		if err := rows.Scan(
			&insight.InsightID,
			&insight.ConversationID,
			&insight.Content,
			&summary,
			&entities,
			&createdAt,
			&lastRecalledAt,
			&insight.RecallCount,
		); err != nil {
			return nil, err
		}
		insight.CreatedAt = time.Unix(createdAt, 0)
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

func (s *PostgresVectorStore) Save(ctx context.Context, memory *UserMemory) error {
	now := time.Now().Unix()
	if memory.CreatedAt.IsZero() {
		memory.CreatedAt = time.Now()
	}
	memory.UpdatedAt = time.Now()
	preferencesJSON, err := json.Marshal(memory.Preferences)
	if err != nil {
		return fmt.Errorf("marshal preferences: %w", err)
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

func (s *PostgresVectorStore) UpdatePreferences(ctx context.Context, userID string, prefs *UserPreferences) error {
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

func (s *PostgresVectorStore) AddInsight(ctx context.Context, userID string, insight *Insight) error {
	if insight.InsightID == "" {
		insight.InsightID = fmt.Sprintf("insight-%d", time.Now().UnixNano())
	}
	insight.UserID = userID
	insight.CreatedAt = time.Now()
	if s.embedder != nil && insight.Content != "" {
		vector, err := s.embedder.EmbedDocuments(ctx, []string{insight.Content})
		if err == nil && len(vector) > 0 {
			insight.Vector = vector[0]
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
				_ = s.vectorStore.Upsert(ctx, []vectorstore.Record{record})
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

func (s *PostgresVectorStore) SearchInsights(ctx context.Context, userID string, query string, limit int) ([]*Insight, error) {
	if limit <= 0 {
		limit = 5
	}
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
				return s.buildInsightsFromSearchResults(results)
			}
		}
	}
	return s.searchInsightsFromDB(ctx, userID, query, limit)
}

func (s *PostgresVectorStore) buildInsightsFromSearchResults(results []vectorstore.SearchResult) ([]*Insight, error) {
	var insights []*Insight
	for _, result := range results {
		entities := strings.Split(result.Citation.DocType, ",")
		content := result.Content
		summaryLen := 100
		if len(content) < summaryLen {
			summaryLen = len(content)
		}
		insights = append(insights, &Insight{
			Content:  content,
			Summary:  content[:summaryLen],
			Entities: entities,
		})
	}
	return insights, nil
}

func (s *PostgresVectorStore) searchInsightsFromDB(ctx context.Context, userID string, query string, limit int) ([]*Insight, error) {
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
		var createdAt int64
		var lastRecalledAt *int64
		if err := rows.Scan(
			&insight.InsightID,
			&insight.ConversationID,
			&insight.Content,
			&summary,
			&entities,
			&createdAt,
			&lastRecalledAt,
			&insight.RecallCount,
		); err != nil {
			return nil, err
		}
		insight.CreatedAt = time.Unix(createdAt, 0)
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

func (s *PostgresVectorStore) RecallInsight(ctx context.Context, insightID string) error {
	_, err := s.db.Exec(ctx, fmt.Sprintf(`
		UPDATE %s
		SET recall_count = recall_count + 1, last_recalled_at = $1
		WHERE insight_id = $2
	`, InsightTable), time.Now().Unix(), insightID)
	return err
}

func (s *PostgresVectorStore) UpdateStockPool(ctx context.Context, userID string, stocks []string) error {
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
		existing := make(map[string]bool)
		for _, st := range memory.StockPool {
			existing[st] = true
		}
		for _, st := range stocks {
			existing[st] = true
		}
		var merged []string
		for st := range existing {
			merged = append(merged, st)
		}
		memory.StockPool = merged
		memory.UpdatedAt = time.Now()
	}
	return s.Save(ctx, memory)
}

func (s *PostgresVectorStore) ExtractAndUpdatePreferences(ctx context.Context, userID string, messages []*repository.Message) error {
	if len(messages) == 0 {
		return nil
	}
	stockMentions := make(map[string]int)
	for _, msg := range messages {
		if msg.Metadata != nil {
			if stockCode, ok := msg.Metadata["stock_code"]; ok {
				stockMentions[fmt.Sprintf("%v", stockCode)]++
			}
		}
	}
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

// AddInterestedStock appends a stock to the user's preference list.
func (s *PostgresVectorStore) AddInterestedStock(ctx context.Context, userID string, stock string) error {
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
	for _, st := range memory.Preferences.InterestedStocks {
		if st == stock {
			return nil
		}
	}
	memory.Preferences.InterestedStocks = append(memory.Preferences.InterestedStocks, stock)
	memory.UpdatedAt = time.Now()
	return s.Save(ctx, memory)
}

// AddPreferredMetric appends a metric to the user's preference list.
func (s *PostgresVectorStore) AddPreferredMetric(ctx context.Context, userID string, metric string) error {
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
	for _, m := range memory.Preferences.PreferredMetrics {
		if m == metric {
			return nil
		}
	}
	memory.Preferences.PreferredMetrics = append(memory.Preferences.PreferredMetrics, metric)
	memory.UpdatedAt = time.Now()
	return s.Save(ctx, memory)
}

// GetUserPreferences returns preferences for a user.
func (s *PostgresVectorStore) GetUserPreferences(ctx context.Context, userID string) (*UserPreferences, error) {
	memory, err := s.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	if memory == nil {
		return nil, nil
	}
	return memory.Preferences, nil
}

// BatchAddInsights 批量添加洞察，用于会话结束时的记忆沉淀
func (s *PostgresVectorStore) BatchAddInsights(ctx context.Context, userID string, insights []*Insight) error {
	if len(insights) == 0 {
		return nil
	}

	// 批量处理向量嵌入和向量存储
	var records []vectorstore.Record
	for _, insight := range insights {
		insight.UserID = userID
		if insight.InsightID == "" {
			insight.InsightID = fmt.Sprintf("insight-%d", time.Now().UnixNano())
		}
		insight.CreatedAt = time.Now()

		// 生成向量嵌入
		if s.embedder != nil && insight.Content != "" {
			vector, err := s.embedder.EmbedDocuments(ctx, []string{insight.Content})
			if err == nil && len(vector) > 0 {
				insight.Vector = vector[0]
				if s.vectorStore != nil {
					records = append(records, vectorstore.Record{
						ID:      insight.InsightID,
						Content: insight.Content,
						Vector:  insight.Vector,
						Metadata: map[string]string{
							"user_id":         userID,
							"conversation_id": insight.ConversationID,
							"entities":        strings.Join(insight.Entities, ","),
						},
					})
				}
			}
		}
	}

	// 批量写入向量存储
	if len(records) > 0 && s.vectorStore != nil {
		if err := s.vectorStore.Upsert(ctx, records); err != nil {
			return fmt.Errorf("upsert vectors: %w", err)
		}
	}

	// 批量写入数据库
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, insight := range insights {
		entities := insight.Entities
		if entities == nil {
			entities = []string{}
		}
		_, err := tx.Exec(ctx, fmt.Sprintf(`
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
		if err != nil {
			return fmt.Errorf("insert insight: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}
