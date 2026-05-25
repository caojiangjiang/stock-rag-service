package short

import (
	"context"
	"errors"
	"time"

	"stock_rag/internal/repository"
)

// ErrNotFound is returned when working memory is not found.
var ErrNotFound = errors.New("short-term memory not found")

// KeyPrefix is the Redis key prefix for working memory.
const KeyPrefix = "working:"

// WorkingMemory holds in-session working state (messages, task draft, entity chain).
type WorkingMemory struct {
	ConversationID string                `json:"conversation_id"`
	UserID         string                `json:"user_id"`
	Messages       []*repository.Message `json:"messages"`
	TaskState      *TaskState            `json:"task_state"`
	EntityChain    []*EntityReference    `json:"entity_chain"`
	CurrentFocus   string                `json:"current_focus"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
}

// TaskState tracks agent task progress within a conversation.
type TaskState struct {
	Goal           string         `json:"goal"`
	CompletedSteps []string       `json:"completed_steps"`
	PendingSteps   []string       `json:"pending_steps"`
	Evidence       map[string]any `json:"evidence"`
	Status         string         `json:"status"`
}

// EntityReference links pronouns to recently mentioned entities.
type EntityReference struct {
	Entity      string    `json:"entity"`
	CanonicalID string    `json:"canonical_id"`
	MentionTime time.Time `json:"mention_time"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// Store is the short-term (working) memory interface.
type Store interface {
	Save(ctx context.Context, memory *WorkingMemory) error
	Get(ctx context.Context, conversationID string) (*WorkingMemory, error)
	AppendMessage(ctx context.Context, conversationID string, msg *repository.Message) error
	UpdateTaskState(ctx context.Context, conversationID string, state *TaskState) error
	AddEntityReference(ctx context.Context, conversationID string, ref *EntityReference) error
	ResolveReference(ctx context.Context, conversationID string, pronoun string) (string, error)
	GetTaskState(ctx context.Context, conversationID string) (*TaskState, error)
	SetCurrentFocus(ctx context.Context, conversationID, focus string) error
	GetCurrentFocus(ctx context.Context, conversationID string) (string, error)
	Cleanup(ctx context.Context, conversationID string) error
	InitSchema(ctx context.Context) error
}
