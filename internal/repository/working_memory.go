// internal/repository/working_memory.go
package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// WorkingMemoryKeyPrefix Redis key prefix for working memory
const WorkingMemoryKeyPrefix = "working:"

// WorkingMemory 短期记忆
type WorkingMemory struct {
	ConversationID string             `json:"conversation_id"`
	UserID         string             `json:"user_id"`
	Messages       []*Message         `json:"messages"`      // 滑动窗口消息
	TaskState      *TaskState         `json:"task_state"`    // Agent 草稿纸
	EntityChain    []*EntityReference `json:"entity_chain"`  // 指代链
	CurrentFocus   string             `json:"current_focus"` // 当前分析对象
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

// TaskState Agent 任务状态
type TaskState struct {
	Goal           string         `json:"goal"`            // 当前目标
	CompletedSteps []string       `json:"completed_steps"` // 已完成步骤
	PendingSteps   []string       `json:"pending_steps"`   // 待完成步骤
	Evidence       map[string]any `json:"evidence"`        // 已提取的证据
	Status         string         `json:"status"`          // running/completed/failed
}

// EntityReference 指代引用
type EntityReference struct {
	Entity      string    `json:"entity"`       // 实体名称：贵州茅台
	CanonicalID string    `json:"canonical_id"` // 标准 ID：600519
	MentionTime time.Time `json:"mention_time"` // 提及时间
	ExpiresAt   time.Time `json:"expires_at"`   // 过期时间
}

// WorkingMemoryStore 短期记忆存储接口
type WorkingMemoryStore interface {
	// Save 保存短期记忆
	Save(ctx context.Context, memory *WorkingMemory) error

	// Get 获取短期记忆
	Get(ctx context.Context, conversationID string) (*WorkingMemory, error)

	// AppendMessage 添加消息到滑动窗口
	AppendMessage(ctx context.Context, conversationID string, msg *Message) error

	// UpdateTaskState 更新任务状态
	UpdateTaskState(ctx context.Context, conversationID string, state *TaskState) error

	// AddEntityReference 添加实体引用（用于指代消解）
	AddEntityReference(ctx context.Context, conversationID string, ref *EntityReference) error

	// ResolveReference 解析指代（查找"它"、"那家"等指代词指向的实体）
	ResolveReference(ctx context.Context, conversationID string, pronoun string) (string, error)

	// GetTaskState 获取任务状态
	GetTaskState(ctx context.Context, conversationID string) (*TaskState, error)

	// SetCurrentFocus 设置当前分析对象
	SetCurrentFocus(ctx context.Context, conversationID string, focus string) error

	// GetCurrentFocus 获取当前分析对象
	GetCurrentFocus(ctx context.Context, conversationID string) (string, error)

	// Cleanup 清理过期记忆
	Cleanup(ctx context.Context, conversationID string) error
}

// RedisWorkingMemoryStore 基于 Redis 的短期记忆存储
type RedisWorkingMemoryStore struct {
	client      *redis.Client
	defaultTTL  time.Duration
	maxMessages int // 滑动窗口最大消息数
}

// NewRedisWorkingMemoryStore 创建基于 Redis 的短期记忆存储
func NewRedisWorkingMemoryStore(client *redis.Client) *RedisWorkingMemoryStore {
	return &RedisWorkingMemoryStore{
		client:      client,
		defaultTTL:  1 * time.Hour, // 1 小时无交互过期
		maxMessages: 20,            // 保留最近 20 条消息
	}
}

// NewRedisWorkingMemoryStoreWithConfig 创建带配置的短期记忆存储
func NewRedisWorkingMemoryStoreWithConfig(client *redis.Client, ttl time.Duration, maxMessages int) *RedisWorkingMemoryStore {
	return &RedisWorkingMemoryStore{
		client:      client,
		defaultTTL:  ttl,
		maxMessages: maxMessages,
	}
}

// redisKey 构建 Redis key
func (s *RedisWorkingMemoryStore) redisKey(conversationID string) string {
	return WorkingMemoryKeyPrefix + conversationID
}

// Save 保存短期记忆
func (s *RedisWorkingMemoryStore) Save(ctx context.Context, memory *WorkingMemory) error {
	memory.UpdatedAt = time.Now()
	data, err := json.Marshal(memory)
	if err != nil {
		return fmt.Errorf("failed to marshal working memory: %w", err)
	}

	return s.client.Set(ctx, s.redisKey(memory.ConversationID), data, s.defaultTTL).Err()
}

// Get 获取短期记忆
func (s *RedisWorkingMemoryStore) Get(ctx context.Context, conversationID string) (*WorkingMemory, error) {
	data, err := s.client.Get(ctx, s.redisKey(conversationID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, ErrNotFound
		}
		return nil, err
	}

	var memory WorkingMemory
	if err := json.Unmarshal(data, &memory); err != nil {
		return nil, fmt.Errorf("failed to unmarshal working memory: %w", err)
	}

	return &memory, nil
}

// AppendMessage 添加消息到滑动窗口
func (s *RedisWorkingMemoryStore) AppendMessage(ctx context.Context, conversationID string, msg *Message) error {
	memory, err := s.Get(ctx, conversationID)
	if err != nil && err != ErrNotFound {
		return err
	}

	if memory == nil {
		// 创建新的短期记忆
		memory = &WorkingMemory{
			ConversationID: conversationID,
			UserID:         msg.UserID,
			Messages:       []*Message{msg},
			EntityChain:    []*EntityReference{},
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
	} else {
		// 添加消息到滑动窗口
		memory.Messages = append(memory.Messages, msg)

		// 保持滑动窗口大小
		if len(memory.Messages) > s.maxMessages {
			memory.Messages = memory.Messages[len(memory.Messages)-s.maxMessages:]
		}

		memory.UpdatedAt = time.Now()
	}

	return s.Save(ctx, memory)
}

// UpdateTaskState 更新任务状态
func (s *RedisWorkingMemoryStore) UpdateTaskState(ctx context.Context, conversationID string, state *TaskState) error {
	memory, err := s.Get(ctx, conversationID)
	if err != nil {
		if err == ErrNotFound {
			// 创建新的记忆（只有 TaskState）
			memory = &WorkingMemory{
				ConversationID: conversationID,
				TaskState:      state,
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			}
		} else {
			return err
		}
	} else {
		memory.TaskState = state
		memory.UpdatedAt = time.Now()
	}

	return s.Save(ctx, memory)
}

// AddEntityReference 添加实体引用
func (s *RedisWorkingMemoryStore) AddEntityReference(ctx context.Context, conversationID string, ref *EntityReference) error {
	memory, err := s.Get(ctx, conversationID)
	if err != nil {
		if err == ErrNotFound {
			memory = &WorkingMemory{
				ConversationID: conversationID,
				EntityChain:    []*EntityReference{ref},
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			}
		} else {
			return err
		}
	} else {
		// 添加新的实体引用
		memory.EntityChain = append(memory.EntityChain, ref)

		// 清理过期的实体引用
		now := time.Now()
		var validRefs []*EntityReference
		for _, r := range memory.EntityChain {
			if r.ExpiresAt.After(now) {
				validRefs = append(validRefs, r)
			}
		}
		memory.EntityChain = validRefs

		memory.UpdatedAt = time.Now()
	}

	return s.Save(ctx, memory)
}

// ResolveReference 解析指代
func (s *RedisWorkingMemoryStore) ResolveReference(ctx context.Context, conversationID string, pronoun string) (string, error) {
	memory, err := s.Get(ctx, conversationID)
	if err != nil {
		return "", err
	}

	if memory == nil || len(memory.EntityChain) == 0 {
		return "", nil
	}

	// 简单实现：返回最近的实体
	// 实际场景中需要更复杂的指代消解逻辑
	now := time.Now()
	var mostRecent *EntityReference
	for _, ref := range memory.EntityChain {
		if ref.ExpiresAt.After(now) {
			if mostRecent == nil || ref.MentionTime.After(mostRecent.MentionTime) {
				mostRecent = ref
			}
		}
	}

	if mostRecent == nil {
		return "", nil
	}

	return mostRecent.Entity, nil
}

// Cleanup 清理过期记忆
func (s *RedisWorkingMemoryStore) Cleanup(ctx context.Context, conversationID string) error {
	memory, err := s.Get(ctx, conversationID)
	if err != nil {
		return err
	}

	if memory == nil {
		return nil
	}

	// 清理过期的实体引用
	now := time.Now()
	var validRefs []*EntityReference
	for _, ref := range memory.EntityChain {
		if ref.ExpiresAt.After(now) {
			validRefs = append(validRefs, ref)
		}
	}
	memory.EntityChain = validRefs

	// 如果没有有效数据，删除整个记忆
	if len(memory.Messages) == 0 && len(memory.EntityChain) == 0 && memory.TaskState == nil {
		return s.client.Del(ctx, s.redisKey(conversationID)).Err()
	}

	return s.Save(ctx, memory)
}

// GetMessages 获取短期记忆中的消息
func (s *RedisWorkingMemoryStore) GetMessages(ctx context.Context, conversationID string) ([]*Message, error) {
	memory, err := s.Get(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if memory == nil {
		return []*Message{}, nil
	}
	return memory.Messages, nil
}

// GetTaskState 获取任务状态
func (s *RedisWorkingMemoryStore) GetTaskState(ctx context.Context, conversationID string) (*TaskState, error) {
	memory, err := s.Get(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if memory == nil {
		return nil, nil
	}
	return memory.TaskState, nil
}

// SetCurrentFocus 设置当前分析对象
func (s *RedisWorkingMemoryStore) SetCurrentFocus(ctx context.Context, conversationID, focus string) error {
	memory, err := s.Get(ctx, conversationID)
	if err != nil && err != ErrNotFound {
		return err
	}

	if memory == nil {
		memory = &WorkingMemory{
			ConversationID: conversationID,
			CurrentFocus:   focus,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
	} else {
		memory.CurrentFocus = focus
		memory.UpdatedAt = time.Now()
	}

	return s.Save(ctx, memory)
}

// GetCurrentFocus 获取当前分析对象
func (s *RedisWorkingMemoryStore) GetCurrentFocus(ctx context.Context, conversationID string) (string, error) {
	memory, err := s.Get(ctx, conversationID)
	if err != nil {
		return "", err
	}
	if memory == nil {
		return "", nil
	}
	return memory.CurrentFocus, nil
}

// IsExpired 检查记忆是否过期（基于 TTL）
func (s *RedisWorkingMemoryStore) IsExpired(ctx context.Context, conversationID string) (bool, error) {
	exists, err := s.client.Exists(ctx, s.redisKey(conversationID)).Result()
	if err != nil {
		return false, err
	}
	return exists == 0, nil
}

// extractEntityFromMessage 从消息中提取实体（简化实现）
func extractEntityFromMessage(content string) string {
	// 简单实现：检测股票代码或公司名称
	// 实际场景中需要 NER 或正则匹配
	content = strings.TrimSpace(content)
	if len(content) > 0 {
		return content
	}
	return ""
}
