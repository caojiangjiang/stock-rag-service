package medium

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned when session context is not found.
var ErrNotFound = errors.New("medium-term memory not found")

// TableName is the PostgreSQL table for session contexts.
const TableName = "session_contexts"

// SessionContext holds medium-term session facts and task progress.
type SessionContext struct {
	ConversationID string                    `json:"conversation_id"`
	UserID         string                    `json:"user_id"`
	ConfirmedFacts map[string]*ConfirmedFact `json:"confirmed_facts"`
	TaskProgress   *TaskProgress             `json:"task_progress"`
	CurrentObjects []string                  `json:"current_objects"`
	TimeRange      string                    `json:"time_range"`
	CreatedAt      time.Time                 `json:"created_at"`
	UpdatedAt      time.Time                 `json:"updated_at"`
	ExpiresAt      time.Time                 `json:"expires_at"`
}

// ConfirmedFact is a verified fact tied to the session.
type ConfirmedFact struct {
	Key           string    `json:"key"`
	Value         any       `json:"value"`
	Unit          string    `json:"unit"`
	Source        string    `json:"source"`
	VerifiedAt    time.Time `json:"verified_at"`
	Verified      bool      `json:"verified"`
	RetrievalNote string    `json:"retrieval_note"`
}

// TaskProgress tracks multi-step task execution within a session.
type TaskProgress struct {
	TaskID      string           `json:"task_id"`
	TaskName    string           `json:"task_name"`
	SubTasks    []*SubTaskStatus `json:"sub_tasks"`
	CurrentStep int              `json:"current_step"`
	TotalSteps  int              `json:"total_steps"`
	Status      TaskStatus       `json:"status"`
}

// SubTaskStatus is the state of a single sub-task.
type SubTaskStatus struct {
	SubTaskID   string    `json:"sub_task_id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Result      any       `json:"result"`
	CompletedAt time.Time `json:"completed_at"`
}

// TaskStatus is the overall task status.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
)

// Store is the medium-term (session) memory interface.
type Store interface {
	Save(ctx context.Context, sessionCtx *SessionContext) error
	Get(ctx context.Context, conversationID string) (*SessionContext, error)
	AddConfirmedFact(ctx context.Context, conversationID string, fact *ConfirmedFact) error
	AddConfirmedFacts(ctx context.Context, conversationID string, facts []*ConfirmedFact) error
	UpdateTaskProgress(ctx context.Context, conversationID string, progress *TaskProgress) error
	CompleteSubTask(ctx context.Context, conversationID string, subTaskID string, result any) error
	GetFactsByEntity(ctx context.Context, conversationID string, entityID string) (map[string]*ConfirmedFact, error)
	HasFact(ctx context.Context, conversationID string, key string) (bool, *ConfirmedFact, error)
	Delete(ctx context.Context, conversationID string) error
	UpdateCurrentObjects(ctx context.Context, conversationID string, objects []string) error
	GetPendingFacts(ctx context.Context, conversationID string) ([]*ConfirmedFact, error)
	InitSchema(ctx context.Context) error
}
