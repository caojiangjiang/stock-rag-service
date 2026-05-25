package short

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"stock_rag/internal/repository"

	"github.com/redis/go-redis/v9"
)

// RedisStore implements Store using Redis.
type RedisStore struct {
	client      *redis.Client
	defaultTTL  time.Duration
	maxMessages int
}

// NewRedisStore creates a Redis-backed working memory store.
func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{
		client:      client,
		defaultTTL:  time.Hour,
		maxMessages: 20,
	}
}

// NewRedisStoreWithConfig creates a Redis store with custom TTL and window size.
func NewRedisStoreWithConfig(client *redis.Client, ttl time.Duration, maxMessages int) *RedisStore {
	return &RedisStore{
		client:      client,
		defaultTTL:  ttl,
		maxMessages: maxMessages,
	}
}

func (s *RedisStore) redisKey(conversationID string) string {
	return KeyPrefix + conversationID
}

func (s *RedisStore) Save(ctx context.Context, memory *WorkingMemory) error {
	memory.UpdatedAt = time.Now()
	data, err := json.Marshal(memory)
	if err != nil {
		return fmt.Errorf("marshal working memory: %w", err)
	}
	return s.client.Set(ctx, s.redisKey(memory.ConversationID), data, s.defaultTTL).Err()
}

func (s *RedisStore) Get(ctx context.Context, conversationID string) (*WorkingMemory, error) {
	data, err := s.client.Get(ctx, s.redisKey(conversationID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var memory WorkingMemory
	if err := json.Unmarshal(data, &memory); err != nil {
		return nil, fmt.Errorf("unmarshal working memory: %w", err)
	}
	return &memory, nil
}

func (s *RedisStore) AppendMessage(ctx context.Context, conversationID string, msg *repository.Message) error {
	memory, err := s.Get(ctx, conversationID)
	if err != nil && err != ErrNotFound {
		return err
	}
	if memory == nil {
		memory = &WorkingMemory{
			ConversationID: conversationID,
			UserID:         msg.UserID,
			Messages:       []*repository.Message{msg},
			EntityChain:    []*EntityReference{},
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
	} else {
		memory.Messages = append(memory.Messages, msg)
		if len(memory.Messages) > s.maxMessages {
			memory.Messages = memory.Messages[len(memory.Messages)-s.maxMessages:]
		}
		memory.UpdatedAt = time.Now()
	}
	return s.Save(ctx, memory)
}

func (s *RedisStore) UpdateTaskState(ctx context.Context, conversationID string, state *TaskState) error {
	memory, err := s.Get(ctx, conversationID)
	if err != nil {
		if err == ErrNotFound {
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

func (s *RedisStore) AddEntityReference(ctx context.Context, conversationID string, ref *EntityReference) error {
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
		memory.EntityChain = append(memory.EntityChain, ref)
		now := time.Now()
		var valid []*EntityReference
		for _, r := range memory.EntityChain {
			if r.ExpiresAt.After(now) {
				valid = append(valid, r)
			}
		}
		memory.EntityChain = valid
		memory.UpdatedAt = time.Now()
	}
	return s.Save(ctx, memory)
}

func (s *RedisStore) ResolveReference(ctx context.Context, conversationID string, _ string) (string, error) {
	memory, err := s.Get(ctx, conversationID)
	if err != nil {
		return "", err
	}
	if memory == nil || len(memory.EntityChain) == 0 {
		return "", nil
	}
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

func (s *RedisStore) Cleanup(ctx context.Context, conversationID string) error {
	memory, err := s.Get(ctx, conversationID)
	if err != nil {
		return err
	}
	if memory == nil {
		return nil
	}
	now := time.Now()
	var valid []*EntityReference
	for _, ref := range memory.EntityChain {
		if ref.ExpiresAt.After(now) {
			valid = append(valid, ref)
		}
	}
	memory.EntityChain = valid
	if len(memory.Messages) == 0 && len(memory.EntityChain) == 0 && memory.TaskState == nil {
		return s.client.Del(ctx, s.redisKey(conversationID)).Err()
	}
	return s.Save(ctx, memory)
}

func (s *RedisStore) GetMessages(ctx context.Context, conversationID string) ([]*repository.Message, error) {
	memory, err := s.Get(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if memory == nil {
		return []*repository.Message{}, nil
	}
	return memory.Messages, nil
}

func (s *RedisStore) GetTaskState(ctx context.Context, conversationID string) (*TaskState, error) {
	memory, err := s.Get(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if memory == nil {
		return nil, nil
	}
	return memory.TaskState, nil
}

func (s *RedisStore) SetCurrentFocus(ctx context.Context, conversationID, focus string) error {
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

func (s *RedisStore) GetCurrentFocus(ctx context.Context, conversationID string) (string, error) {
	memory, err := s.Get(ctx, conversationID)
	if err != nil {
		return "", err
	}
	if memory == nil {
		return "", nil
	}
	return memory.CurrentFocus, nil
}

// IsExpired reports whether the Redis key for the conversation has expired.
func (s *RedisStore) IsExpired(ctx context.Context, conversationID string) (bool, error) {
	exists, err := s.client.Exists(ctx, s.redisKey(conversationID)).Result()
	if err != nil {
		return false, err
	}
	return exists == 0, nil
}

// InitSchema is a no-op for Redis as it doesn't require table initialization.
func (s *RedisStore) InitSchema(ctx context.Context) error {
	return nil
}
