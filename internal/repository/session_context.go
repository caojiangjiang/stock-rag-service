// internal/repository/session_context.go
package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
)

// SessionContextKeyPrefix PostgreSQL table prefix
const SessionContextTable = "session_contexts"

// SessionContext 中期记忆
type SessionContext struct {
	ConversationID string                    `json:"conversation_id"`
	UserID         string                    `json:"user_id"`
	ConfirmedFacts map[string]*ConfirmedFact `json:"confirmed_facts"` // 已确认的事实
	TaskProgress   *TaskProgress             `json:"task_progress"`   // 当前任务进度
	CurrentObjects []string                  `json:"current_objects"` // 当前分析对象列表
	TimeRange      string                    `json:"time_range"`      // 时间范围
	CreatedAt      time.Time                 `json:"created_at"`
	UpdatedAt      time.Time                 `json:"updated_at"`
	ExpiresAt      time.Time                 `json:"expires_at"` // TTL：24 小时
}

// ConfirmedFact 已确认事实
type ConfirmedFact struct {
	Key           string    `json:"key"`            // 如："600519_2023_revenue"
	Value         any       `json:"value"`          // 值：如 1400（单位：亿元）
	Unit          string    `json:"unit"`           // 单位
	Source        string    `json:"source"`         // 来源：如 "2023年报"
	VerifiedAt    time.Time `json:"verified_at"`    // 确认时间
	Verified      bool      `json:"verified"`       // 是否已验证
	RetrievalNote string    `json:"retrieval_note"` // 检索备注
}

// TaskProgress 任务进度
type TaskProgress struct {
	TaskID      string           `json:"task_id"`      // 任务 ID
	TaskName    string           `json:"task_name"`    // 任务名称
	SubTasks    []*SubTaskStatus `json:"sub_tasks"`    // 子任务状态
	CurrentStep int              `json:"current_step"` // 当前步骤
	TotalSteps  int              `json:"total_steps"`  // 总步骤数
	Status      TaskStatus       `json:"status"`       // pending/running/completed/failed
}

// SubTaskStatus 子任务状态
type SubTaskStatus struct {
	SubTaskID   string    `json:"sub_task_id"`  // 子任务 ID
	Name        string    `json:"name"`         // 子任务名称
	Status      string    `json:"status"`       // pending/completed/failed
	Result      any       `json:"result"`       // 执行结果
	CompletedAt time.Time `json:"completed_at"` // 完成时间
}

// TaskStatus 任务状态枚举
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
)

// SessionContextStore 中期记忆存储接口
type SessionContextStore interface {
	// Save 保存或更新会话上下文
	Save(ctx context.Context, sessionCtx *SessionContext) error

	// Get 获取会话上下文
	Get(ctx context.Context, conversationID string) (*SessionContext, error)

	// AddConfirmedFact 添加已确认事实
	AddConfirmedFact(ctx context.Context, conversationID string, fact *ConfirmedFact) error

	// AddConfirmedFacts 批量添加已确认事实
	AddConfirmedFacts(ctx context.Context, conversationID string, facts []*ConfirmedFact) error

	// UpdateTaskProgress 更新任务进度
	UpdateTaskProgress(ctx context.Context, conversationID string, progress *TaskProgress) error

	// CompleteSubTask 完成子任务
	CompleteSubTask(ctx context.Context, conversationID string, subTaskID string, result any) error

	// GetFactsByEntity 获取指定实体的所有事实
	GetFactsByEntity(ctx context.Context, conversationID string, entityID string) (map[string]*ConfirmedFact, error)

	// HasFact 检查事实是否已存在
	HasFact(ctx context.Context, conversationID string, key string) (bool, *ConfirmedFact, error)

	// Delete 删除会话上下文
	Delete(ctx context.Context, conversationID string) error

	// UpdateCurrentObjects 更新当前分析对象列表
	UpdateCurrentObjects(ctx context.Context, conversationID string, objects []string) error

	// GetPendingFacts 获取所有待验证事实
	GetPendingFacts(ctx context.Context, conversationID string) ([]*ConfirmedFact, error)
}

// PostgresSessionContextStore 基于 PostgreSQL JSONB 的中期记忆存储
type PostgresSessionContextStore struct {
	db         *pgxpool.Pool
	defaultTTL time.Duration
}

// NewPostgresSessionContextStore 创建基于 PostgreSQL 的中期记忆存储
func NewPostgresSessionContextStore(db *pgxpool.Pool) *PostgresSessionContextStore {
	return &PostgresSessionContextStore{
		db:         db,
		defaultTTL: 24 * time.Hour, // 默认 24 小时过期
	}
}

// NewPostgresSessionContextStoreWithTTL 创建带 TTL 配置的中期记忆存储
func NewPostgresSessionContextStoreWithTTL(db *pgxpool.Pool, ttl time.Duration) *PostgresSessionContextStore {
	return &PostgresSessionContextStore{
		db:         db,
		defaultTTL: ttl,
	}
}

// InitTables 初始化表结构
func (s *PostgresSessionContextStore) InitTables(ctx context.Context) error {
	_, err := s.db.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			conversation_id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			context JSONB NOT NULL,
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL,
			expires_at BIGINT
		)
	`, SessionContextTable))
	if err != nil {
		return fmt.Errorf("failed to create session_contexts table: %w", err)
	}

	// 创建索引
	_, err = s.db.Exec(ctx, fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_session_contexts_user_id ON %s (user_id)
	`, SessionContextTable))
	if err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	_, err = s.db.Exec(ctx, fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_session_contexts_expires_at ON %s (expires_at)
	`, SessionContextTable))
	if err != nil {
		return fmt.Errorf("failed to create expires_at index: %w", err)
	}

	return nil
}

// Save 保存或更新会话上下文
func (s *PostgresSessionContextStore) Save(ctx context.Context, sessionCtx *SessionContext) error {
	if sessionCtx.CreatedAt.IsZero() {
		sessionCtx.CreatedAt = time.Now()
	}
	sessionCtx.UpdatedAt = time.Now()

	if sessionCtx.ExpiresAt.IsZero() {
		sessionCtx.ExpiresAt = time.Now().Add(s.defaultTTL)
	}

	contextJSON, err := json.Marshal(sessionCtx)
	if err != nil {
		return fmt.Errorf("failed to marshal session context: %w", err)
	}

	_, err = s.db.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (conversation_id, user_id, context, created_at, updated_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (conversation_id) DO UPDATE SET
			context = $3,
			updated_at = $5,
			expires_at = $6
	`, SessionContextTable),
		sessionCtx.ConversationID,
		sessionCtx.UserID,
		contextJSON,
		sessionCtx.CreatedAt.Unix(),
		sessionCtx.UpdatedAt.Unix(),
		sessionCtx.ExpiresAt.Unix())

	return err
}

// Get 获取会话上下文
func (s *PostgresSessionContextStore) Get(ctx context.Context, conversationID string) (*SessionContext, error) {
	var contextJSON []byte
	var expiresAt int64

	err := s.db.QueryRow(ctx, fmt.Sprintf(`
		SELECT context, expires_at FROM %s WHERE conversation_id = $1
	`, SessionContextTable), conversationID).Scan(&contextJSON, &expiresAt)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// 检查是否过期
	if expiresAt > 0 {
		if time.Now().Unix() > expiresAt {
			// 已过期，删除并返回 NotFound
			go s.Delete(context.Background(), conversationID)
			return nil, ErrNotFound
		}
	}

	var sessionCtx SessionContext
	if err := json.Unmarshal(contextJSON, &sessionCtx); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session context: %w", err)
	}

	return &sessionCtx, nil
}

// AddConfirmedFact 添加已确认事实
func (s *PostgresSessionContextStore) AddConfirmedFact(ctx context.Context, conversationID string, fact *ConfirmedFact) error {
	sessionCtx, err := s.Get(ctx, conversationID)
	if err != nil && err != ErrNotFound {
		return err
	}

	if sessionCtx == nil {
		// 创建新的会话上下文
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

	// 添加或更新事实
	fact.VerifiedAt = time.Now()
	fact.Verified = true
	sessionCtx.ConfirmedFacts[fact.Key] = fact

	return s.Save(ctx, sessionCtx)
}

// AddConfirmedFacts 批量添加已确认事实
func (s *PostgresSessionContextStore) AddConfirmedFacts(ctx context.Context, conversationID string, facts []*ConfirmedFact) error {
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

// UpdateTaskProgress 更新任务进度
func (s *PostgresSessionContextStore) UpdateTaskProgress(ctx context.Context, conversationID string, progress *TaskProgress) error {
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

// CompleteSubTask 完成子任务
func (s *PostgresSessionContextStore) CompleteSubTask(ctx context.Context, conversationID string, subTaskID string, result any) error {
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

	// 更新当前步骤
	sessionCtx.TaskProgress.CurrentStep++

	// 检查是否所有子任务都完成
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

// GetFactsByEntity 获取指定实体的所有事实
func (s *PostgresSessionContextStore) GetFactsByEntity(ctx context.Context, conversationID string, entityID string) (map[string]*ConfirmedFact, error) {
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

// HasFact 检查事实是否已存在
func (s *PostgresSessionContextStore) HasFact(ctx context.Context, conversationID string, key string) (bool, *ConfirmedFact, error) {
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

// Delete 删除会话上下文
func (s *PostgresSessionContextStore) Delete(ctx context.Context, conversationID string) error {
	_, err := s.db.Exec(ctx, fmt.Sprintf(`
		DELETE FROM %s WHERE conversation_id = $1
	`, SessionContextTable), conversationID)
	return err
}

// UpdateCurrentObjects 更新当前分析对象列表
func (s *PostgresSessionContextStore) UpdateCurrentObjects(ctx context.Context, conversationID string, objects []string) error {
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

// AddPendingFact 添加待验证事实（未验证的事实）
func (s *PostgresSessionContextStore) AddPendingFact(ctx context.Context, conversationID string, fact *ConfirmedFact) error {
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

// VerifyFact 验证事实
func (s *PostgresSessionContextStore) VerifyFact(ctx context.Context, conversationID string, key string, value any) error {
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

	// 更新值和验证状态
	fact.Value = value
	fact.Verified = true
	fact.VerifiedAt = time.Now()

	sessionCtx.UpdatedAt = time.Now()
	return s.Save(ctx, sessionCtx)
}

// GetPendingFacts 获取所有待验证事实
func (s *PostgresSessionContextStore) GetPendingFacts(ctx context.Context, conversationID string) ([]*ConfirmedFact, error) {
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
