package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"stock_rag/internal/auth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AuthSession struct {
	ID               string    `json:"id"`
	UserID           string    `json:"user_id"`
	SessionTokenHash string    `json:"session_token_hash"`
	JWTJTI           string    `json:"jwt_jti,omitempty"`
	ClientInfo       string    `json:"client_info,omitempty"`
	IP               string    `json:"ip,omitempty"`
	UserAgent        string    `json:"user_agent,omitempty"`
	LastActiveAt     time.Time `json:"last_active_at"`
	ExpiresAt        time.Time `json:"expires_at"`
	RevokedAt        time.Time `json:"revoked_at,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type ConversationSummary struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Version        int       `json:"version"`
	SummaryType    string    `json:"summary_type"` // rolling/milestone/route_context
	SummaryText    string    `json:"summary_text"`
	SummaryJSON    string    `json:"summary_json"` // 结构化摘要 JSON
	SourceStartSeq int64     `json:"source_start_seq"`
	SourceEndSeq   int64     `json:"source_end_seq"`
	TokenCount     int       `json:"token_count"`
	ModelName      string    `json:"model_name,omitempty"`
	Status         string    `json:"status"`     // pending/ready/failed
	CreatedBy      string    `json:"created_by"` // async_worker/manual/system
	CreatedAt      time.Time `json:"created_at"`
}

type RouteDecision struct {
	ID                string    `json:"id"`
	ConversationID    string    `json:"conversation_id"`
	MessageID         string    `json:"message_id"`
	ClassifierType    string    `json:"classifier_type"` // rule/llm/hybrid
	ClassifierVersion string    `json:"classifier_version"`
	PredictedMode     string    `json:"predicted_mode"` // chat/rag/agent/analysis
	Confidence        float64   `json:"confidence"`
	Reason            string    `json:"reason,omitempty"`
	Candidates        string    `json:"candidates"` // JSON array
	Selected          bool      `json:"selected"`
	CreatedAt         time.Time `json:"created_at"`
}

// CoordinatorDecision 协调器决策记录（与 RouteDecision 通过 MessageID 关联）
type CoordinatorDecision struct {
	ID                string    `json:"id"`
	ConversationID    string    `json:"conversation_id"`
	MessageID         string    `json:"message_id"`      // 关联 RouteDecision
	ClassifierType    string    `json:"classifier_type"` // rule/llm/stickiness/complexity/default
	ClassifierVersion string    `json:"classifier_version"`
	PredictedType     string    `json:"predicted_type"` // plan/supervisor/pipeline/workflow/debate/committee/peer/deep
	SelectedType      string    `json:"selected_type"`
	Confidence        float64   `json:"confidence"`
	Reason            string    `json:"reason,omitempty"`
	Candidates        string    `json:"candidates"` // JSON array
	ComplexityScore   float64   `json:"complexity_score"`
	TriggeredFallback bool      `json:"triggered_fallback"`
	UserFollowUp      bool      `json:"user_follow_up"`
	LatencyMs         int       `json:"latency_ms"`
	CreatedAt         time.Time `json:"created_at"`
}

type ConversationJob struct {
	ID             string    `json:"id"`
	JobType        string    `json:"job_type"` // summarize/rewrite/route/title
	ConversationID string    `json:"conversation_id"`
	MessageID      string    `json:"message_id,omitempty"`
	Payload        string    `json:"payload"` // JSON
	Status         string    `json:"status"`  // pending/running/success/failed
	AttemptCount   int       `json:"attempt_count"`
	MaxAttempts    int       `json:"max_attempts"`
	NextRunAt      time.Time `json:"next_run_at"`
	LockedAt       time.Time `json:"locked_at,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type PostgresAuthRepositoryV2 struct {
	pool *pgxpool.Pool
}

func NewPostgresAuthRepositoryV2(pool *pgxpool.Pool) *PostgresAuthRepositoryV2 {
	return &PostgresAuthRepositoryV2{pool: pool}
}

func (r *PostgresAuthRepositoryV2) InitAllTables(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			username VARCHAR(64) UNIQUE NOT NULL,
			email VARCHAR(255) UNIQUE NOT NULL,
			password_hash VARCHAR(255) NOT NULL,
			status VARCHAR(20) DEFAULT 'active',
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS auth_sessions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			session_token_hash VARCHAR(255),
			jwt_jti VARCHAR(128),
			client_info JSONB,
			ip INET,
			user_agent TEXT,
			last_active_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMPTZ NOT NULL,
			revoked_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS conversations (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title VARCHAR(255),
			status VARCHAR(20) DEFAULT 'active',
			mode_hint VARCHAR(20),
			last_route_mode VARCHAR(20),
			active_summary_version INT DEFAULT 0,
			message_count INT DEFAULT 0,
			total_input_tokens INT DEFAULT 0,
			total_output_tokens INT DEFAULT 0,
			context_token_budget INT DEFAULT 8000,
			last_message_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			archived_at TIMESTAMPTZ
		)`,

		`CREATE TABLE IF NOT EXISTS conversation_messages (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
			user_id UUID NOT NULL,
			seq BIGINT NOT NULL,
			role VARCHAR(20) NOT NULL,
			message_type VARCHAR(30) DEFAULT 'chat',
			content TEXT NOT NULL DEFAULT '',
			content_json JSONB,
			parent_message_id UUID,
			route_mode VARCHAR(20),
			tool_name VARCHAR(100),
			tool_calls JSONB,
			tool_results JSONB,
			citations JSONB,
			model_name VARCHAR(100),
			input_tokens INT DEFAULT 0,
			output_tokens INT DEFAULT 0,
			latency_ms INT DEFAULT 0,
			status VARCHAR(20) DEFAULT 'received',
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(conversation_id, seq)
		)`,

		`CREATE TABLE IF NOT EXISTS conversation_summaries (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
			version INT NOT NULL,
			summary_type VARCHAR(30) DEFAULT 'rolling',
			summary_text TEXT,
			summary_json JSONB,
			source_start_seq BIGINT,
			source_end_seq BIGINT,
			token_count INT DEFAULT 0,
			model_name VARCHAR(100),
			status VARCHAR(20) DEFAULT 'pending',
			created_by VARCHAR(30) DEFAULT 'async_worker',
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(conversation_id, version)
		)`,

		`CREATE TABLE IF NOT EXISTS route_decisions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
			message_id UUID NOT NULL,
			classifier_type VARCHAR(20) DEFAULT 'llm',
			classifier_version VARCHAR(50),
			predicted_mode VARCHAR(20) NOT NULL,
			confidence DECIMAL(5,4) DEFAULT 0,
			reason TEXT,
			candidates JSONB,
			selected BOOLEAN DEFAULT true,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS conversation_jobs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			job_type VARCHAR(30) NOT NULL,
			conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
			message_id UUID,
			payload JSONB,
			status VARCHAR(20) DEFAULT 'pending',
			attempt_count INT DEFAULT 0,
			max_attempts INT DEFAULT 3,
			next_run_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			locked_at TIMESTAMPTZ,
			last_error TEXT,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE INDEX IF NOT EXISTS idx_auth_sessions_user_id ON auth_sessions(user_id, last_active_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_sessions_expires ON auth_sessions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_sessions_revoked ON auth_sessions(revoked_at)`,

		`CREATE INDEX IF NOT EXISTS idx_conversations_user_id ON conversations(user_id, last_message_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_conversations_status ON conversations(status, updated_at DESC)`,

		`CREATE INDEX IF NOT EXISTS idx_messages_conversation_id ON conversation_messages(conversation_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_user_id ON conversation_messages(user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_route_mode ON conversation_messages(route_mode, created_at DESC)`,

		`CREATE INDEX IF NOT EXISTS idx_summaries_conversation_id ON conversation_summaries(conversation_id, status, version DESC)`,

		`CREATE INDEX IF NOT EXISTS idx_route_message_id ON route_decisions(message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_route_conversation_id ON route_decisions(conversation_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_route_predicted_mode ON route_decisions(predicted_mode, created_at DESC)`,

		`CREATE INDEX IF NOT EXISTS idx_jobs_status ON conversation_jobs(status, next_run_at)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_conversation_id ON conversation_jobs(conversation_id, created_at DESC)`,

		`CREATE INDEX IF NOT EXISTS idx_windows_user_id ON windows(user_id)`,
	}

	for _, q := range queries {
		if _, err := r.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}
	return nil
}

func (r *PostgresAuthRepositoryV2) CreateUser(user *auth.User) error {
	ctx := context.Background()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO users (id, username, email, password_hash, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, uuid.New().String(), user.Username, user.Email, user.Password, "active", time.Now(), time.Now())
	return err
}

func (r *PostgresAuthRepositoryV2) GetUserByID(id string) (*auth.User, bool) {
	ctx := context.Background()
	row := r.pool.QueryRow(ctx, `SELECT id, username, email, password_hash, status, created_at, updated_at FROM users WHERE id = $1`, id)

	var user auth.User
	var status string
	err := row.Scan(&user.ID, &user.Username, &user.Email, &user.Password, &status, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, false
	}
	return &user, true
}

func (r *PostgresAuthRepositoryV2) GetUserByUsername(username string) (*auth.User, bool) {
	ctx := context.Background()
	row := r.pool.QueryRow(ctx, `SELECT id, username, email, password_hash, status, created_at, updated_at FROM users WHERE username = $1`, username)

	var user auth.User
	var status string
	err := row.Scan(&user.ID, &user.Username, &user.Email, &user.Password, &status, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, false
	}
	return &user, true
}

func (r *PostgresAuthRepositoryV2) CreateAuthSession(session *AuthSession) error {
	ctx := context.Background()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO auth_sessions (id, user_id, session_token_hash, jwt_jti, client_info, ip, user_agent, last_active_at, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, session.ID, session.UserID, session.SessionTokenHash, session.JWTJTI, session.ClientInfo,
		session.IP, session.UserAgent, session.LastActiveAt, session.ExpiresAt, session.CreatedAt)
	return err
}

func (r *PostgresAuthRepositoryV2) GetAuthSessionByJTI(jti string) (*AuthSession, bool) {
	ctx := context.Background()
	row := r.pool.QueryRow(ctx, `
		SELECT id, user_id, session_token_hash, jwt_jti, client_info, ip, user_agent, last_active_at, expires_at, revoked_at, created_at
		FROM auth_sessions WHERE jwt_jti = $1 AND revoked_at IS NULL AND expires_at > CURRENT_TIMESTAMP
	`, jti)

	var session AuthSession
	var clientInfo *string
	var ip, userAgent, revokedAt *string
	err := row.Scan(&session.ID, &session.UserID, &session.SessionTokenHash, &session.JWTJTI,
		&clientInfo, &ip, &userAgent, &session.LastActiveAt, &session.ExpiresAt, &revokedAt, &session.CreatedAt)
	if err != nil {
		return nil, false
	}
	if clientInfo != nil {
		session.ClientInfo = *clientInfo
	}
	if ip != nil {
		session.IP = *ip
	}
	if userAgent != nil {
		session.UserAgent = *userAgent
	}
	return &session, true
}

func (r *PostgresAuthRepositoryV2) UpdateAuthSessionActive(sessionID string) error {
	ctx := context.Background()
	_, err := r.pool.Exec(ctx, `UPDATE auth_sessions SET last_active_at = $1 WHERE id = $2`, time.Now(), sessionID)
	return err
}

func (r *PostgresAuthRepositoryV2) RevokeAuthSession(sessionID string) error {
	ctx := context.Background()
	_, err := r.pool.Exec(ctx, `UPDATE auth_sessions SET revoked_at = $1 WHERE id = $2`, time.Now(), sessionID)
	return err
}

func (r *PostgresAuthRepositoryV2) CreateConversation(conv *ConversationV2) error {
	ctx := context.Background()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO conversations (id, user_id, title, status, mode_hint, context_token_budget, message_count, last_message_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, conv.ID, conv.UserID, conv.Title, conv.Status, conv.ModeHint, conv.ContextTokenBudget, 0, nil, conv.CreatedAt, conv.UpdatedAt)
	return err
}

func (r *PostgresAuthRepositoryV2) GetConversationByID(id string) (*ConversationV2, bool) {
	ctx := context.Background()
	row := r.pool.QueryRow(ctx, `
		SELECT id, user_id, title, status, mode_hint, last_route_mode, active_summary_version,
		       message_count, total_input_tokens, total_output_tokens, context_token_budget,
		       last_message_at, created_at, updated_at, archived_at
		FROM conversations WHERE id = $1
	`, id)

	var conv ConversationV2
	err := row.Scan(&conv.ID, &conv.UserID, &conv.Title, &conv.Status, &conv.ModeHint, &conv.LastRouteMode,
		&conv.ActiveSummaryVersion, &conv.MessageCount, &conv.TotalInputTokens, &conv.TotalOutputTokens,
		&conv.ContextTokenBudget, &conv.LastMessageAt, &conv.CreatedAt, &conv.UpdatedAt, &conv.ArchivedAt)
	if err != nil {
		return nil, false
	}
	return &conv, true
}

func (r *PostgresAuthRepositoryV2) GetConversationsByUserID(userID string, limit, offset int) ([]*ConversationV2, int) {
	ctx := context.Background()

	var total int
	r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM conversations WHERE user_id = $1 AND status != 'deleted'`, userID).Scan(&total)

	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, title, status, mode_hint, last_route_mode, active_summary_version,
		       message_count, total_input_tokens, total_output_tokens, context_token_budget,
		       last_message_at, created_at, updated_at, archived_at
		FROM conversations WHERE user_id = $1 AND status != 'deleted'
		ORDER BY last_message_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		return []*ConversationV2{}, total
	}
	defer rows.Close()

	var conversations []*ConversationV2
	for rows.Next() {
		var conv ConversationV2
		rows.Scan(&conv.ID, &conv.UserID, &conv.Title, &conv.Status, &conv.ModeHint, &conv.LastRouteMode,
			&conv.ActiveSummaryVersion, &conv.MessageCount, &conv.TotalInputTokens, &conv.TotalOutputTokens,
			&conv.ContextTokenBudget, &conv.LastMessageAt, &conv.CreatedAt, &conv.UpdatedAt, &conv.ArchivedAt)
		conversations = append(conversations, &conv)
	}

	return conversations, total
}

func (r *PostgresAuthRepositoryV2) UpdateConversation(conv *ConversationV2) error {
	ctx := context.Background()
	_, err := r.pool.Exec(ctx, `
		UPDATE conversations SET title = $1, status = $2, mode_hint = $3, last_route_mode = $4,
			active_summary_version = $5, message_count = $6, total_input_tokens = $7, total_output_tokens = $8,
			context_token_budget = $9, last_message_at = $10, updated_at = $11, archived_at = $12
		WHERE id = $13
	`, conv.Title, conv.Status, conv.ModeHint, conv.LastRouteMode, conv.ActiveSummaryVersion,
		conv.MessageCount, conv.TotalInputTokens, conv.TotalOutputTokens, conv.ContextTokenBudget,
		conv.LastMessageAt, conv.UpdatedAt, conv.ArchivedAt, conv.ID)
	return err
}

func (r *PostgresAuthRepositoryV2) DeleteConversation(id string) error {
	ctx := context.Background()
	_, err := r.pool.Exec(ctx, `UPDATE conversations SET status = 'deleted', archived_at = $1 WHERE id = $2`, time.Now(), id)
	return err
}

func (r *PostgresAuthRepositoryV2) CreateMessage(msg *MessageV2) error {
	ctx := context.Background()
	toolCallsJSON, _ := json.Marshal(msg.ToolCalls)
	toolResultsJSON, _ := json.Marshal(msg.ToolResults)
	citationsJSON, _ := json.Marshal(msg.Citations)
	contentJSON, _ := json.Marshal(msg.ContentJSON)

	_, err := r.pool.Exec(ctx, `
		INSERT INTO conversation_messages (id, conversation_id, user_id, seq, role, message_type, content, content_json,
			parent_message_id, route_mode, tool_name, tool_calls, tool_results, citations, model_name,
			input_tokens, output_tokens, latency_ms, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
	`, msg.ID, msg.ConversationID, msg.UserID, msg.Seq, msg.Role, msg.MessageType, msg.Content, contentJSON,
		msg.ParentMessageID, msg.RouteMode, msg.ToolName, toolCallsJSON, toolResultsJSON, citationsJSON,
		msg.ModelName, msg.InputTokens, msg.OutputTokens, msg.LatencyMs, msg.Status, msg.CreatedAt)
	return err
}

func (r *PostgresAuthRepositoryV2) GetMessagesByConversationID(conversationID string, limit, offset int) ([]*MessageV2, error) {
	ctx := context.Background()
	rows, err := r.pool.Query(ctx, `
		SELECT id, conversation_id, user_id, seq, role, message_type, content, content_json,
		       parent_message_id, route_mode, tool_name, tool_calls, tool_results, citations,
		       model_name, input_tokens, output_tokens, latency_ms, status, created_at
		FROM conversation_messages
		WHERE conversation_id = $1
		ORDER BY seq ASC
		LIMIT $2 OFFSET $3
	`, conversationID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*MessageV2
	for rows.Next() {
		var msg MessageV2
		var toolCallsJSON, toolResultsJSON, citationsJSON, contentJSON []byte
		err := rows.Scan(&msg.ID, &msg.ConversationID, &msg.UserID, &msg.Seq, &msg.Role, &msg.MessageType,
			&msg.Content, &contentJSON, &msg.ParentMessageID, &msg.RouteMode, &msg.ToolName,
			&toolCallsJSON, &toolResultsJSON, &citationsJSON, &msg.ModelName, &msg.InputTokens,
			&msg.OutputTokens, &msg.LatencyMs, &msg.Status, &msg.CreatedAt)
		if err != nil {
			return nil, err
		}
		json.Unmarshal(toolCallsJSON, &msg.ToolCalls)
		json.Unmarshal(toolResultsJSON, &msg.ToolResults)
		json.Unmarshal(citationsJSON, &msg.Citations)
		json.Unmarshal(contentJSON, &msg.ContentJSON)
		messages = append(messages, &msg)
	}
	return messages, nil
}

func (r *PostgresAuthRepositoryV2) GetLastMessageSeq(conversationID string) (int64, error) {
	ctx := context.Background()
	var seq int64
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(seq), 0) FROM conversation_messages WHERE conversation_id = $1
	`, conversationID).Scan(&seq)
	return seq, err
}

func (r *PostgresAuthRepositoryV2) CreateSummary(summary *ConversationSummary) error {
	ctx := context.Background()
	summaryJSON, _ := json.Marshal(summary.SummaryJSON)
	_, err := r.pool.Exec(ctx, `
		INSERT INTO conversation_summaries (id, conversation_id, version, summary_type, summary_text, summary_json,
			source_start_seq, source_end_seq, token_count, model_name, status, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`, summary.ID, summary.ConversationID, summary.Version, summary.SummaryType, summary.SummaryText, summaryJSON,
		summary.SourceStartSeq, summary.SourceEndSeq, summary.TokenCount, summary.ModelName, summary.Status, summary.CreatedBy, summary.CreatedAt)
	return err
}

func (r *PostgresAuthRepositoryV2) GetLatestSummary(conversationID string) (*ConversationSummary, bool) {
	ctx := context.Background()
	row := r.pool.QueryRow(ctx, `
		SELECT id, conversation_id, version, summary_type, summary_text, summary_json,
		       source_start_seq, source_end_seq, token_count, model_name, status, created_by, created_at
		FROM conversation_summaries
		WHERE conversation_id = $1 AND status = 'ready'
		ORDER BY version DESC
		LIMIT 1
	`, conversationID)

	var summary ConversationSummary
	var summaryJSON []byte
	err := row.Scan(&summary.ID, &summary.ConversationID, &summary.Version, &summary.SummaryType,
		&summary.SummaryText, &summaryJSON, &summary.SourceStartSeq, &summary.SourceEndSeq,
		&summary.TokenCount, &summary.ModelName, &summary.Status, &summary.CreatedBy, &summary.CreatedAt)
	if err != nil {
		return nil, false
	}
	json.Unmarshal(summaryJSON, &summary.SummaryJSON)
	return &summary, true
}

func (r *PostgresAuthRepositoryV2) GetSummariesByConversationID(conversationID string) ([]*ConversationSummary, error) {
	ctx := context.Background()
	rows, err := r.pool.Query(ctx, `
		SELECT id, conversation_id, version, summary_type, summary_text, summary_json,
		       source_start_seq, source_end_seq, token_count, model_name, status, created_by, created_at
		FROM conversation_summaries
		WHERE conversation_id = $1
		ORDER BY version DESC
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []*ConversationSummary
	for rows.Next() {
		var summary ConversationSummary
		var summaryJSON []byte
		rows.Scan(&summary.ID, &summary.ConversationID, &summary.Version, &summary.SummaryType,
			&summary.SummaryText, &summaryJSON, &summary.SourceStartSeq, &summary.SourceEndSeq,
			&summary.TokenCount, &summary.ModelName, &summary.Status, &summary.CreatedBy, &summary.CreatedAt)
		json.Unmarshal(summaryJSON, &summary.SummaryJSON)
		summaries = append(summaries, &summary)
	}
	return summaries, nil
}

func (r *PostgresAuthRepositoryV2) UpdateSummaryStatus(summaryID, status string) error {
	ctx := context.Background()
	_, err := r.pool.Exec(ctx, `UPDATE conversation_summaries SET status = $1 WHERE id = $2`, status, summaryID)
	return err
}

func (r *PostgresAuthRepositoryV2) CreateRouteDecision(decision *RouteDecision) error {
	ctx := context.Background()
	candidatesJSON, _ := json.Marshal(decision.Candidates)
	_, err := r.pool.Exec(ctx, `
		INSERT INTO route_decisions (id, conversation_id, message_id, classifier_type, classifier_version,
			predicted_mode, confidence, reason, candidates, selected, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, decision.ID, decision.ConversationID, decision.MessageID, decision.ClassifierType,
		decision.ClassifierVersion, decision.PredictedMode, decision.Confidence, decision.Reason,
		candidatesJSON, decision.Selected, decision.CreatedAt)
	return err
}

// CreateCoordinatorDecision 创建协调器决策记录（与 RouteDecision 通过 MessageID 关联）
func (r *PostgresAuthRepositoryV2) CreateCoordinatorDecision(decision *CoordinatorDecision) error {
	ctx := context.Background()
	candidatesJSON, _ := json.Marshal(decision.Candidates)
	_, err := r.pool.Exec(ctx, `
		INSERT INTO coordinator_decisions (id, conversation_id, message_id, classifier_type, classifier_version,
			predicted_type, selected_type, confidence, reason, candidates, complexity_score,
			triggered_fallback, user_follow_up, latency_ms, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`, decision.ID, decision.ConversationID, decision.MessageID, decision.ClassifierType,
		decision.ClassifierVersion, decision.PredictedType, decision.SelectedType,
		decision.Confidence, decision.Reason, candidatesJSON, decision.ComplexityScore,
		decision.TriggeredFallback, decision.UserFollowUp, decision.LatencyMs, decision.CreatedAt)
	return err
}

func (r *PostgresAuthRepositoryV2) CreateJob(job *ConversationJob) error {
	ctx := context.Background()
	payloadJSON, _ := json.Marshal(job.Payload)
	_, err := r.pool.Exec(ctx, `
		INSERT INTO conversation_jobs (id, job_type, conversation_id, message_id, payload, status,
			attempt_count, max_attempts, next_run_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, job.ID, job.JobType, job.ConversationID, job.MessageID, payloadJSON, job.Status,
		job.AttemptCount, job.MaxAttempts, job.NextRunAt, job.CreatedAt, job.UpdatedAt)
	return err
}

func (r *PostgresAuthRepositoryV2) GetPendingJobs(limit int) ([]*ConversationJob, error) {
	ctx := context.Background()
	rows, err := r.pool.Query(ctx, `
		SELECT id, job_type, conversation_id, message_id, payload, status,
		       attempt_count, max_attempts, next_run_at, locked_at, last_error, created_at, updated_at
		FROM conversation_jobs
		WHERE status = 'pending' AND next_run_at <= CURRENT_TIMESTAMP
		ORDER BY next_run_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*ConversationJob
	for rows.Next() {
		var job ConversationJob
		var payloadJSON []byte
		var lockedAt *time.Time
		rows.Scan(&job.ID, &job.JobType, &job.ConversationID, &job.MessageID, &payloadJSON,
			&job.Status, &job.AttemptCount, &job.MaxAttempts, &job.NextRunAt, &lockedAt,
			&job.LastError, &job.CreatedAt, &job.UpdatedAt)
		json.Unmarshal(payloadJSON, &job.Payload)
		if lockedAt != nil {
			job.LockedAt = *lockedAt
		}
		jobs = append(jobs, &job)
	}
	return jobs, nil
}

func (r *PostgresAuthRepositoryV2) UpdateJobStatus(jobID, status, lastError string) error {
	ctx := context.Background()
	_, err := r.pool.Exec(ctx, `
		UPDATE conversation_jobs SET status = $1, last_error = $2, updated_at = $3 WHERE id = $4
	`, status, lastError, time.Now(), jobID)
	return err
}

func (r *PostgresAuthRepositoryV2) LockJob(jobID string) error {
	ctx := context.Background()
	_, err := r.pool.Exec(ctx, `
		UPDATE conversation_jobs SET status = 'running', locked_at = $1, updated_at = $1 WHERE id = $2
	`, time.Now(), jobID)
	return err
}

func (r *PostgresAuthRepositoryV2) ReleaseJob(jobID string) error {
	ctx := context.Background()
	_, err := r.pool.Exec(ctx, `
		UPDATE conversation_jobs SET status = 'pending', locked_at = NULL, updated_at = $1 WHERE id = $2
	`, time.Now(), jobID)
	return err
}

func (r *PostgresAuthRepositoryV2) IncrementJobAttempt(jobID string) error {
	ctx := context.Background()
	_, err := r.pool.Exec(ctx, `
		UPDATE conversation_jobs SET attempt_count = attempt_count + 1, status = 'pending',
			next_run_at = CURRENT_TIMESTAMP + (attempt_count + 1) * interval '1 minute',
			locked_at = NULL, updated_at = $1 WHERE id = $2
	`, time.Now(), jobID)
	return err
}

type ConversationV2 struct {
	ID                   string     `json:"id"`
	UserID               string     `json:"user_id"`
	Title                string     `json:"title"`
	Status               string     `json:"status"`
	ModeHint             string     `json:"mode_hint,omitempty"`
	LastRouteMode        string     `json:"last_route_mode,omitempty"`
	ActiveSummaryVersion int        `json:"active_summary_version"`
	MessageCount         int        `json:"message_count"`
	TotalInputTokens     int        `json:"total_input_tokens"`
	TotalOutputTokens    int        `json:"total_output_tokens"`
	ContextTokenBudget   int        `json:"context_token_budget"`
	LastMessageAt        *time.Time `json:"last_message_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	ArchivedAt           *time.Time `json:"archived_at,omitempty"`
}

type MessageV2 struct {
	ID              string                   `json:"id"`
	ConversationID  string                   `json:"conversation_id"`
	UserID          string                   `json:"user_id"`
	Seq             int64                    `json:"seq"`
	Role            string                   `json:"role"`
	MessageType     string                   `json:"message_type"`
	Content         string                   `json:"content"`
	ContentJSON     map[string]interface{}   `json:"content_json,omitempty"`
	ParentMessageID string                   `json:"parent_message_id,omitempty"`
	RouteMode       string                   `json:"route_mode,omitempty"`
	ToolName        string                   `json:"tool_name,omitempty"`
	ToolCalls       []map[string]interface{} `json:"tool_calls,omitempty"`
	ToolResults     []map[string]interface{} `json:"tool_results,omitempty"`
	Citations       []map[string]interface{} `json:"citations,omitempty"`
	ModelName       string                   `json:"model_name,omitempty"`
	InputTokens     int                      `json:"input_tokens"`
	OutputTokens    int                      `json:"output_tokens"`
	LatencyMs       int                      `json:"latency_ms"`
	Status          string                   `json:"status"`
	CreatedAt       time.Time                `json:"created_at"`
}

type WindowV2 struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	Title          string    `json:"title"`
	ConversationID string    `json:"conversation_id"`
	IsActive       bool      `json:"is_active"`
	CreatedAt      time.Time `json:"created_at"`
	LastActiveAt   time.Time `json:"last_active_at"`
}
