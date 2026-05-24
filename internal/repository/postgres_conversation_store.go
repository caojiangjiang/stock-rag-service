package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"stock_rag/internal/pkgctx"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

type PostgresConversationStore struct {
	db *pgxpool.Pool
}

func NewPostgresConversationStore(host, port, user, password, database, sslmode string) (*PostgresConversationStore, error) {
	if sslmode == "" {
		sslmode = "disable"
	}
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, database, sslmode)

	// 配置连接池参数
	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, err
	}
	// 连接池配置
	config.MaxConns = 25                       // 最大连接数
	config.MinConns = 5                        // 最小连接数
	config.MaxConnLifetime = 5 * time.Minute   // 连接最大生命周期
	config.MaxConnIdleTime = 10 * time.Minute  // 连接最大空闲时间
	config.HealthCheckPeriod = 1 * time.Minute // 健康检查周期

	db, err := pgxpool.ConnectConfig(context.Background(), config)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(context.Background()); err != nil {
		return nil, err
	}

	return &PostgresConversationStore{db: db}, nil
}

// DB 返回底层的数据库连接池
func (p *PostgresConversationStore) DB() *pgxpool.Pool {
	return p.db
}

func (p *PostgresConversationStore) InitTables(ctx context.Context) error {
	_, err := p.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS conversations (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			title TEXT NOT NULL,
			status TEXT,
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL,
			last_message_at BIGINT
		)
	`)
	if err != nil {
		return err
	}

	_, err = p.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL REFERENCES conversations(id),
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			route_mode TEXT,
			metadata JSONB,
			created_at BIGINT NOT NULL,
			FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		return err
	}

	_, err = p.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS conversation_contexts (
			conversation_id TEXT PRIMARY KEY REFERENCES conversations(id),
			context JSONB NOT NULL,
			updated_at BIGINT NOT NULL,
			FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		return err
	}

	return nil
}

func (p *PostgresConversationStore) SaveConversation(ctx context.Context, conversation *Conversation) error {
	_, err := p.db.Exec(ctx, `
		INSERT INTO conversations (id, user_id, title, status, created_at, updated_at, last_message_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET
			title = $3,
			status = $4,
			updated_at = $6,
			last_message_at = $7
	`, conversation.ID, conversation.UserID, conversation.Title,
		conversation.Status, conversation.CreatedAt, conversation.UpdatedAt, conversation.LastMessageAt)

	return err
}

func (p *PostgresConversationStore) GetConversation(ctx context.Context, conversationID string) (*Conversation, error) {
	row := p.db.QueryRow(ctx, `
		SELECT id, user_id, title, status, created_at, updated_at, last_message_at
		FROM conversations WHERE id = $1
	`, conversationID)

	var conversation Conversation
	err := row.Scan(
		&conversation.ID,
		&conversation.UserID,
		&conversation.Title,
		&conversation.Status,
		&conversation.CreatedAt,
		&conversation.UpdatedAt,
		&conversation.LastMessageAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &conversation, nil
}

func (p *PostgresConversationStore) ListConversations(ctx context.Context, userID string, limit, offset int) ([]*Conversation, error) {
	return p.GetConversationsByUserID(ctx, userID, limit, offset)
}

func (p *PostgresConversationStore) GetConversationsByUserID(ctx context.Context, userID string, limit, offset int) ([]*Conversation, error) {
	rows, err := p.db.Query(ctx, `
		SELECT id, user_id, title, status, created_at, updated_at, last_message_at
		FROM conversations WHERE user_id = $1
		ORDER BY updated_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*Conversation
	for rows.Next() {
		var conversation Conversation
		err := rows.Scan(
			&conversation.ID,
			&conversation.UserID,
			&conversation.Title,
			&conversation.Status,
			&conversation.CreatedAt,
			&conversation.UpdatedAt,
			&conversation.LastMessageAt,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, &conversation)
	}

	return result, nil
}

func (p *PostgresConversationStore) DeleteConversation(ctx context.Context, conversationID string) error {
	_, err := p.db.Exec(ctx, `DELETE FROM conversations WHERE id = $1`, conversationID)
	return err
}

func (p *PostgresConversationStore) SaveMessage(ctx context.Context, message *Message) error {
	if message.ID == "" {
		message.ID = fmt.Sprintf("msg-%d", time.Now().UnixNano())
	}

	now := time.Now().Unix()
	if message.CreatedAt == 0 {
		message.CreatedAt = now
	}

	metadataJSON, err := json.Marshal(message.Metadata)
	if err != nil {
		return err
	}

	_, err = p.db.Exec(ctx, `
		INSERT INTO messages (id, conversation_id, user_id, role, content, route_mode, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET
			content = $5,
			route_mode = $6,
			metadata = $7
	`, message.ID, message.ConversationID, message.UserID, message.Role, message.Content,
		message.RouteMode, metadataJSON, message.CreatedAt)

	if err != nil {
		return err
	}

	return nil
}

func (p *PostgresConversationStore) GetMessages(ctx context.Context, conversationID string, limit int) ([]*Message, error) {
	rows, err := p.db.Query(ctx, `
		SELECT id, conversation_id, role, content, route_mode, metadata, created_at
		FROM (
			SELECT id, conversation_id, role, content, route_mode, metadata, created_at
			FROM messages WHERE conversation_id = $1
			ORDER BY created_at DESC
			LIMIT $2
		) recent_messages
		ORDER BY created_at ASC
	`, conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*Message
	for rows.Next() {
		var msg Message
		var metadataJSON []byte
		err := rows.Scan(
			&msg.ID,
			&msg.ConversationID,
			&msg.Role,
			&msg.Content,
			&msg.RouteMode,
			&metadataJSON,
			&msg.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		if metadataJSON != nil {
			err := json.Unmarshal(metadataJSON, &msg.Metadata)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, &msg)
	}

	return result, nil
}

func (p *PostgresConversationStore) DeleteMessages(ctx context.Context, conversationID string) error {
	_, err := p.db.Exec(ctx, `DELETE FROM messages WHERE conversation_id = $1`, conversationID)
	return err
}

func (p *PostgresConversationStore) GetMessagesByConversationID(ctx context.Context, conversationID string, limit, offset int) ([]*Message, error) {
	rows, err := p.db.Query(ctx, `
		SELECT id, conversation_id, role, content, route_mode, metadata, created_at
		FROM messages WHERE conversation_id = $1
		ORDER BY created_at ASC
		LIMIT $2 OFFSET $3
	`, conversationID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*Message
	for rows.Next() {
		var msg Message
		var metadataJSON []byte
		err := rows.Scan(
			&msg.ID,
			&msg.ConversationID,
			&msg.Role,
			&msg.Content,
			&msg.RouteMode,
			&metadataJSON,
			&msg.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		if metadataJSON != nil {
			err := json.Unmarshal(metadataJSON, &msg.Metadata)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, &msg)
	}

	return result, nil
}

func (p *PostgresConversationStore) SaveSummary(ctx context.Context, conversationID string, summary *pkgctx.ConversationSummary) error {
	return nil
}

func (p *PostgresConversationStore) GetSummary(ctx context.Context, conversationID string) (*pkgctx.ConversationSummary, error) {
	return nil, ErrNotFound
}

func (p *PostgresConversationStore) SaveContext(ctx context.Context, conversationID string, context *pkgctx.TaskContext) error {
	contextJSON, err := json.Marshal(context)
	if err != nil {
		return err
	}

	_, err = p.db.Exec(ctx, `
		INSERT INTO conversation_contexts (conversation_id, context, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (conversation_id) DO UPDATE SET
			context = $2,
			updated_at = $3
	`, conversationID, contextJSON, time.Now().Unix())

	return err
}

func (p *PostgresConversationStore) GetContext(ctx context.Context, conversationID string) (*pkgctx.TaskContext, error) {
	row := p.db.QueryRow(ctx, `
		SELECT context FROM conversation_contexts WHERE conversation_id = $1
	`, conversationID)

	var contextJSON []byte
	err := row.Scan(&contextJSON)
	if err != nil {
		return nil, err
	}

	var context pkgctx.TaskContext
	err = json.Unmarshal(contextJSON, &context)
	if err != nil {
		return nil, err
	}

	return &context, nil
}

func (p *PostgresConversationStore) UpdateContext(ctx context.Context, conversationID string, updates map[string]interface{}) error {
	row := p.db.QueryRow(ctx, `
		SELECT context FROM conversation_contexts WHERE conversation_id = $1
	`, conversationID)

	var contextJSON []byte
	err := row.Scan(&contextJSON)
	if err != nil {
		if err == pgx.ErrNoRows {
			// 如果没有上下文，创建一个新的
			newCtx := pkgctx.NewTaskContext()
			newCtx.ConversationID = conversationID
			return p.SaveContext(ctx, conversationID, newCtx)
		}
		return err
	}

	var context pkgctx.TaskContext
	err = json.Unmarshal(contextJSON, &context)
	if err != nil {
		return err
	}

	for k, v := range updates {
		switch k {
		case "stock_code":
			context.StockCode, _ = v.(string)
		case "company_name":
			context.CompanyName, _ = v.(string)
		case "time_range":
			context.TimeRange, _ = v.(string)
		case "last_user_intent":
			context.LastUserIntent, _ = v.(string)
		case "output_format":
			context.OutputFormat, _ = v.(string)
		case "is_comparison":
			context.IsComparison, _ = v.(bool)
		case "confirmed_fact":
			if s, ok := v.(string); ok && s != "" {
				context.ConversationSummary = ensureSummary(context.ConversationSummary)
				context.ConversationSummary.ConfirmedFacts = appendUnique(context.ConversationSummary.ConfirmedFacts, s)
			}
		case "pending_question":
			if s, ok := v.(string); ok && s != "" {
				context.ConversationSummary = ensureSummary(context.ConversationSummary)
				context.ConversationSummary.PendingQuestions = appendUnique(context.ConversationSummary.PendingQuestions, s)
			}
		case "current_object":
			if s, ok := v.(string); ok {
				context.ConversationSummary = ensureSummary(context.ConversationSummary)
				context.ConversationSummary.CurrentObject = s
			}
		default:
			if context.CustomFilters == nil {
				context.CustomFilters = make(map[string]string)
			}
			context.CustomFilters[k], _ = v.(string)
		}
	}

	context.UpdatedAt = time.Now()
	return p.SaveContext(ctx, conversationID, &context)
}

func (p *PostgresConversationStore) UpdateLastRouteMode(ctx context.Context, conversationID string, routeMode string) error {
	return nil
}

func (p *PostgresConversationStore) GetLastRouteMode(ctx context.Context, conversationID string) (string, error) {
	row := p.db.QueryRow(ctx, `
		SELECT route_mode
		FROM messages
		WHERE conversation_id = $1 AND route_mode <> ''
		ORDER BY created_at DESC
		LIMIT 1
	`, conversationID)

	var routeMode string
	if err := row.Scan(&routeMode); err != nil {
		if err == pgx.ErrNoRows {
			return "", ErrNotFound
		}
		return "", err
	}
	if routeMode == "" {
		return "", ErrNotFound
	}
	return routeMode, nil
}

func (p *PostgresConversationStore) Close() {
	p.db.Close()
}
