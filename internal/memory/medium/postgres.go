package medium

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
)

// PostgresStore implements Store using PostgreSQL JSONB.
type PostgresStore struct {
	db         *pgxpool.Pool
	defaultTTL time.Duration
}

// NewPostgresStore creates a Postgres-backed session context store.
func NewPostgresStore(db *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{
		db:         db,
		defaultTTL: 24 * time.Hour,
	}
}

// NewPostgresStoreWithTTL creates a store with a custom TTL.
func NewPostgresStoreWithTTL(db *pgxpool.Pool, ttl time.Duration) *PostgresStore {
	return &PostgresStore{
		db:         db,
		defaultTTL: ttl,
	}
}

// InitSchema creates session_contexts table and indexes.
func (s *PostgresStore) InitSchema(ctx context.Context) error {
	_, err := s.db.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			conversation_id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			context JSONB NOT NULL,
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL,
			expires_at BIGINT
		)
	`, TableName))
	if err != nil {
		return fmt.Errorf("create session_contexts table: %w", err)
	}
	_, err = s.db.Exec(ctx, fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_session_contexts_user_id ON %s (user_id)
	`, TableName))
	if err != nil {
		return fmt.Errorf("create user_id index: %w", err)
	}
	_, err = s.db.Exec(ctx, fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_session_contexts_expires_at ON %s (expires_at)
	`, TableName))
	if err != nil {
		return fmt.Errorf("create expires_at index: %w", err)
	}
	return nil
}

func (s *PostgresStore) Save(ctx context.Context, sessionCtx *SessionContext) error {
	if sessionCtx.CreatedAt.IsZero() {
		sessionCtx.CreatedAt = time.Now()
	}
	sessionCtx.UpdatedAt = time.Now()
	if sessionCtx.ExpiresAt.IsZero() {
		sessionCtx.ExpiresAt = time.Now().Add(s.defaultTTL)
	}
	contextJSON, err := json.Marshal(sessionCtx)
	if err != nil {
		return fmt.Errorf("marshal session context: %w", err)
	}
	_, err = s.db.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (conversation_id, user_id, context, created_at, updated_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (conversation_id) DO UPDATE SET
			context = $3,
			updated_at = $5,
			expires_at = $6
	`, TableName),
		sessionCtx.ConversationID,
		sessionCtx.UserID,
		contextJSON,
		sessionCtx.CreatedAt.Unix(),
		sessionCtx.UpdatedAt.Unix(),
		sessionCtx.ExpiresAt.Unix())
	return err
}

func (s *PostgresStore) Get(ctx context.Context, conversationID string) (*SessionContext, error) {
	var contextJSON []byte
	var expiresAt int64
	err := s.db.QueryRow(ctx, fmt.Sprintf(`
		SELECT context, expires_at FROM %s WHERE conversation_id = $1
	`, TableName), conversationID).Scan(&contextJSON, &expiresAt)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if expiresAt > 0 && time.Now().Unix() > expiresAt {
		go s.Delete(context.Background(), conversationID)
		return nil, ErrNotFound
	}
	var sessionCtx SessionContext
	if err := json.Unmarshal(contextJSON, &sessionCtx); err != nil {
		return nil, fmt.Errorf("unmarshal session context: %w", err)
	}
	return &sessionCtx, nil
}

func (s *PostgresStore) AddConfirmedFact(ctx context.Context, conversationID string, fact *ConfirmedFact) error {
	sessionCtx, err := s.Get(ctx, conversationID)
	if err != nil && err != ErrNotFound {
		return err
	}
	if sessionCtx == nil {
		sessionCtx = &SessionContext{
			ConversationID: conversationID,
			ConfirmedFacts: make(map[string]*ConfirmedFact),
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
			ExpiresAt:      time.Now().Add(s.defaultTTL),
		}
	}
	if sessionCtx.ConfirmedFacts == nil {
		sessionCtx.ConfirmedFacts = make(map[string]*ConfirmedFact)
	}
	fact.VerifiedAt = time.Now()
	fact.Verified = true
	sessionCtx.ConfirmedFacts[fact.Key] = fact
	return s.Save(ctx, sessionCtx)
}

func (s *PostgresStore) AddConfirmedFacts(ctx context.Context, conversationID string, facts []*ConfirmedFact) error {
	sessionCtx, err := s.Get(ctx, conversationID)
	if err != nil && err != ErrNotFound {
		return err
	}
	if sessionCtx == nil {
		sessionCtx = &SessionContext{
			ConversationID: conversationID,
			ConfirmedFacts: make(map[string]*ConfirmedFact),
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
			ExpiresAt:      time.Now().Add(s.defaultTTL),
		}
	}
	if sessionCtx.ConfirmedFacts == nil {
		sessionCtx.ConfirmedFacts = make(map[string]*ConfirmedFact)
	}
	now := time.Now()
	for _, fact := range facts {
		fact.VerifiedAt = now
		fact.Verified = true
		sessionCtx.ConfirmedFacts[fact.Key] = fact
	}
	return s.Save(ctx, sessionCtx)
}

func (s *PostgresStore) UpdateTaskProgress(ctx context.Context, conversationID string, progress *TaskProgress) error {
	sessionCtx, err := s.Get(ctx, conversationID)
	if err != nil && err != ErrNotFound {
		return err
	}
	if sessionCtx == nil {
		sessionCtx = &SessionContext{
			ConversationID: conversationID,
			TaskProgress:   progress,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
			ExpiresAt:      time.Now().Add(s.defaultTTL),
		}
	} else {
		sessionCtx.TaskProgress = progress
		sessionCtx.UpdatedAt = time.Now()
	}
	return s.Save(ctx, sessionCtx)
}

func (s *PostgresStore) CompleteSubTask(ctx context.Context, conversationID string, subTaskID string, result any) error {
	sessionCtx, err := s.Get(ctx, conversationID)
	if err != nil {
		return err
	}
	if sessionCtx == nil || sessionCtx.TaskProgress == nil {
		return fmt.Errorf("task progress not found")
	}
	for _, subTask := range sessionCtx.TaskProgress.SubTasks {
		if subTask.SubTaskID == subTaskID {
			subTask.Status = "completed"
			subTask.Result = result
			subTask.CompletedAt = time.Now()
			break
		}
	}
	sessionCtx.TaskProgress.CurrentStep++
	allCompleted := true
	for _, subTask := range sessionCtx.TaskProgress.SubTasks {
		if subTask.Status != "completed" {
			allCompleted = false
			break
		}
	}
	if allCompleted {
		sessionCtx.TaskProgress.Status = TaskStatusCompleted
	}
	sessionCtx.UpdatedAt = time.Now()
	return s.Save(ctx, sessionCtx)
}

func (s *PostgresStore) GetFactsByEntity(ctx context.Context, conversationID string, entityID string) (map[string]*ConfirmedFact, error) {
	sessionCtx, err := s.Get(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if sessionCtx == nil || sessionCtx.ConfirmedFacts == nil {
		return make(map[string]*ConfirmedFact), nil
	}
	result := make(map[string]*ConfirmedFact)
	prefix := entityID + "_"
	for key, fact := range sessionCtx.ConfirmedFacts {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			result[key] = fact
		}
	}
	return result, nil
}

func (s *PostgresStore) HasFact(ctx context.Context, conversationID string, key string) (bool, *ConfirmedFact, error) {
	sessionCtx, err := s.Get(ctx, conversationID)
	if err != nil {
		if err == ErrNotFound {
			return false, nil, nil
		}
		return false, nil, err
	}
	if sessionCtx == nil || sessionCtx.ConfirmedFacts == nil {
		return false, nil, nil
	}
	fact, exists := sessionCtx.ConfirmedFacts[key]
	return exists, fact, nil
}

func (s *PostgresStore) Delete(ctx context.Context, conversationID string) error {
	_, err := s.db.Exec(ctx, fmt.Sprintf(`
		DELETE FROM %s WHERE conversation_id = $1
	`, TableName), conversationID)
	return err
}

func (s *PostgresStore) UpdateCurrentObjects(ctx context.Context, conversationID string, objects []string) error {
	sessionCtx, err := s.Get(ctx, conversationID)
	if err != nil && err != ErrNotFound {
		return err
	}
	if sessionCtx == nil {
		sessionCtx = &SessionContext{
			ConversationID: conversationID,
			CurrentObjects: objects,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
			ExpiresAt:      time.Now().Add(s.defaultTTL),
		}
	} else {
		sessionCtx.CurrentObjects = objects
		sessionCtx.UpdatedAt = time.Now()
	}
	return s.Save(ctx, sessionCtx)
}

// AddPendingFact stores an unverified fact.
func (s *PostgresStore) AddPendingFact(ctx context.Context, conversationID string, fact *ConfirmedFact) error {
	sessionCtx, err := s.Get(ctx, conversationID)
	if err != nil && err != ErrNotFound {
		return err
	}
	if sessionCtx == nil {
		sessionCtx = &SessionContext{
			ConversationID: conversationID,
			ConfirmedFacts: make(map[string]*ConfirmedFact),
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
			ExpiresAt:      time.Now().Add(s.defaultTTL),
		}
	}
	if sessionCtx.ConfirmedFacts == nil {
		sessionCtx.ConfirmedFacts = make(map[string]*ConfirmedFact)
	}
	fact.Verified = false
	sessionCtx.ConfirmedFacts[fact.Key] = fact
	return s.Save(ctx, sessionCtx)
}

// VerifyFact marks a fact as verified.
func (s *PostgresStore) VerifyFact(ctx context.Context, conversationID string, key string, value any) error {
	sessionCtx, err := s.Get(ctx, conversationID)
	if err != nil {
		return err
	}
	if sessionCtx == nil || sessionCtx.ConfirmedFacts == nil {
		return fmt.Errorf("fact not found")
	}
	fact, exists := sessionCtx.ConfirmedFacts[key]
	if !exists {
		return fmt.Errorf("fact %s not found", key)
	}
	fact.Value = value
	fact.Verified = true
	fact.VerifiedAt = time.Now()
	sessionCtx.UpdatedAt = time.Now()
	return s.Save(ctx, sessionCtx)
}

func (s *PostgresStore) GetPendingFacts(ctx context.Context, conversationID string) ([]*ConfirmedFact, error) {
	sessionCtx, err := s.Get(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if sessionCtx == nil || sessionCtx.ConfirmedFacts == nil {
		return []*ConfirmedFact{}, nil
	}
	var pending []*ConfirmedFact
	for _, fact := range sessionCtx.ConfirmedFacts {
		if !fact.Verified {
			pending = append(pending, fact)
		}
	}
	return pending, nil
}
