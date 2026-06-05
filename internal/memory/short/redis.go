package short

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"stock_rag/internal/repository"

	"github.com/redis/go-redis/v9"
)

// Redis key layout per conversation:
//   working:{id}:meta  — HASH: task_state, entity_chain, current_focus, user_id, timestamps
//   working:{id}:msgs  — LIST: JSON-serialized messages (RPUSH + LTRIM sliding window)
//
// Hash 适合元数据字段；有序消息用 List，避免整包 JSON 读写竞争。
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

func (s *RedisStore) metaKey(conversationID string) string {
	return KeyPrefix + conversationID + ":meta"
}

func (s *RedisStore) msgsKey(conversationID string) string {
	return KeyPrefix + conversationID + ":msgs"
}

func (s *RedisStore) legacyKey(conversationID string) string {
	return KeyPrefix + conversationID
}

func (s *RedisStore) touchTTL(ctx context.Context, conversationID string) {
	_ = s.client.Expire(ctx, s.metaKey(conversationID), s.defaultTTL).Err()
	_ = s.client.Expire(ctx, s.msgsKey(conversationID), s.defaultTTL).Err()
}

func (s *RedisStore) Save(ctx context.Context, memory *WorkingMemory) error {
	if memory == nil {
		return fmt.Errorf("working memory is nil")
	}
	memory.UpdatedAt = time.Now()
	if memory.CreatedAt.IsZero() {
		memory.CreatedAt = memory.UpdatedAt
	}
	if err := s.saveMeta(ctx, memory); err != nil {
		return err
	}
	if err := s.replaceMessageList(ctx, memory.ConversationID, memory.Messages); err != nil {
		return err
	}
	s.touchTTL(ctx, memory.ConversationID)
	return nil
}

func (s *RedisStore) Get(ctx context.Context, conversationID string) (*WorkingMemory, error) {
	meta, err := s.loadMeta(ctx, conversationID)
	if err != nil {
		if err == ErrNotFound {
			return s.migrateLegacyKey(ctx, conversationID)
		}
		return nil, err
	}
	msgs, err := s.loadMessageList(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	meta.Messages = msgs
	return meta, nil
}

func (s *RedisStore) AppendMessage(ctx context.Context, conversationID string, msg *repository.Message) error {
	if msg == nil {
		return fmt.Errorf("message is nil")
	}
	meta, err := s.loadMeta(ctx, conversationID)
	if err != nil && err != ErrNotFound {
		return err
	}
	now := time.Now()
	if meta == nil {
		meta = &WorkingMemory{
			ConversationID: conversationID,
			UserID:         msg.UserID,
			EntityChain:    []*EntityReference{},
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := s.saveMeta(ctx, meta); err != nil {
			return err
		}
	} else {
		meta.UpdatedAt = now
		if err := s.saveMeta(ctx, meta); err != nil {
			return err
		}
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	pipe := s.client.Pipeline()
	pipe.RPush(ctx, s.msgsKey(conversationID), data)
	pipe.LTrim(ctx, s.msgsKey(conversationID), int64(-s.maxMessages), -1)
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	s.touchTTL(ctx, conversationID)
	return nil
}

func (s *RedisStore) SyncMessages(ctx context.Context, conversationID string, messages []*repository.Message) error {
	if conversationID == "" || len(messages) == 0 {
		return nil
	}
	if len(messages) > s.maxMessages {
		messages = messages[len(messages)-s.maxMessages:]
	}
	meta, err := s.loadMeta(ctx, conversationID)
	if err != nil && err != ErrNotFound {
		return err
	}
	now := time.Now()
	if meta == nil {
		meta = &WorkingMemory{
			ConversationID: conversationID,
			UserID:         messages[len(messages)-1].UserID,
			EntityChain:    []*EntityReference{},
			CreatedAt:      now,
			UpdatedAt:      now,
		}
	} else {
		meta.UpdatedAt = now
	}
	if err := s.saveMeta(ctx, meta); err != nil {
		return err
	}
	if err := s.replaceMessageList(ctx, conversationID, messages); err != nil {
		return err
	}
	s.touchTTL(ctx, conversationID)
	return nil
}

func (s *RedisStore) HasMessages(ctx context.Context, conversationID string) (bool, error) {
	n, err := s.client.LLen(ctx, s.msgsKey(conversationID)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *RedisStore) UpdateTaskState(ctx context.Context, conversationID string, state *TaskState) error {
	meta, err := s.ensureMeta(ctx, conversationID)
	if err != nil {
		return err
	}
	meta.TaskState = state
	meta.UpdatedAt = time.Now()
	return s.saveMeta(ctx, meta)
}

func (s *RedisStore) AddEntityReference(ctx context.Context, conversationID string, ref *EntityReference) error {
	meta, err := s.ensureMeta(ctx, conversationID)
	if err != nil {
		return err
	}
	meta.EntityChain = append(meta.EntityChain, ref)
	now := time.Now()
	var valid []*EntityReference
	for _, r := range meta.EntityChain {
		if r.ExpiresAt.After(now) {
			valid = append(valid, r)
		}
	}
	meta.EntityChain = valid
	meta.UpdatedAt = now
	return s.saveMeta(ctx, meta)
}

func (s *RedisStore) GetRecentEntities(ctx context.Context, conversationID string, limit int) ([]*EntityReference, error) {
	meta, err := s.loadMeta(ctx, conversationID)
	if err != nil {
		if err == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	if meta == nil || len(meta.EntityChain) == 0 {
		return nil, nil
	}

	now := time.Now()
	var validRefs []*EntityReference
	for _, ref := range meta.EntityChain {
		if ref.ExpiresAt.After(now) {
			validRefs = append(validRefs, ref)
		}
	}

	sort.Slice(validRefs, func(i, j int) bool {
		return validRefs[j].MentionTime.Before(validRefs[i].MentionTime)
	})

	if limit > 0 && len(validRefs) > limit {
		validRefs = validRefs[:limit]
	}
	return validRefs, nil
}

func (s *RedisStore) Cleanup(ctx context.Context, conversationID string) error {
	meta, err := s.loadMeta(ctx, conversationID)
	if err == ErrNotFound {
		return s.client.Del(ctx, s.legacyKey(conversationID)).Err()
	}
	if err != nil {
		return err
	}
	now := time.Now()
	var valid []*EntityReference
	for _, ref := range meta.EntityChain {
		if ref.ExpiresAt.After(now) {
			valid = append(valid, ref)
		}
	}
	meta.EntityChain = valid

	hasMsgs, _ := s.HasMessages(ctx, conversationID)
	if !hasMsgs && len(meta.EntityChain) == 0 && meta.TaskState == nil && meta.CurrentFocus == "" {
		pipe := s.client.Pipeline()
		pipe.Del(ctx, s.metaKey(conversationID))
		pipe.Del(ctx, s.msgsKey(conversationID))
		pipe.Del(ctx, s.legacyKey(conversationID))
		_, err := pipe.Exec(ctx)
		return err
	}
	return s.saveMeta(ctx, meta)
}

func (s *RedisStore) GetMessages(ctx context.Context, conversationID string) ([]*repository.Message, error) {
	msgs, err := s.loadMessageList(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		if wm, legacyErr := s.migrateLegacyKey(ctx, conversationID); legacyErr == nil && wm != nil {
			return wm.Messages, nil
		}
		return nil, ErrNotFound
	}
	return msgs, nil
}

func (s *RedisStore) GetTaskState(ctx context.Context, conversationID string) (*TaskState, error) {
	meta, err := s.loadMeta(ctx, conversationID)
	if err != nil {
		if err == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	if meta == nil {
		return nil, nil
	}
	return meta.TaskState, nil
}

func (s *RedisStore) SetCurrentFocus(ctx context.Context, conversationID, focus string) error {
	meta, err := s.ensureMeta(ctx, conversationID)
	if err != nil {
		return err
	}
	meta.CurrentFocus = focus
	meta.UpdatedAt = time.Now()
	return s.saveMeta(ctx, meta)
}

func (s *RedisStore) GetCurrentFocus(ctx context.Context, conversationID string) (string, error) {
	meta, err := s.loadMeta(ctx, conversationID)
	if err != nil {
		if err == ErrNotFound {
			return "", nil
		}
		return "", err
	}
	if meta == nil {
		return "", nil
	}
	return meta.CurrentFocus, nil
}

// IsExpired reports whether working memory keys have expired.
func (s *RedisStore) IsExpired(ctx context.Context, conversationID string) (bool, error) {
	n, err := s.client.Exists(ctx, s.metaKey(conversationID), s.msgsKey(conversationID), s.legacyKey(conversationID)).Result()
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

func (s *RedisStore) InitSchema(ctx context.Context) error {
	return nil
}

func (s *RedisStore) ensureMeta(ctx context.Context, conversationID string) (*WorkingMemory, error) {
	meta, err := s.loadMeta(ctx, conversationID)
	if err != nil && err != ErrNotFound {
		return nil, err
	}
	if meta != nil {
		return meta, nil
	}
	now := time.Now()
	meta = &WorkingMemory{
		ConversationID: conversationID,
		EntityChain:    []*EntityReference{},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.saveMeta(ctx, meta); err != nil {
		return nil, err
	}
	return meta, nil
}

func (s *RedisStore) loadMeta(ctx context.Context, conversationID string) (*WorkingMemory, error) {
	values, err := s.client.HGetAll(ctx, s.metaKey(conversationID)).Result()
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, ErrNotFound
	}

	meta := &WorkingMemory{
		ConversationID: conversationID,
		UserID:         values["user_id"],
		CurrentFocus:   values["current_focus"],
		EntityChain:    []*EntityReference{},
	}
	if ts, ok := values["created_at"]; ok {
		if v, parseErr := strconv.ParseInt(ts, 10, 64); parseErr == nil {
			meta.CreatedAt = time.Unix(v, 0)
		}
	}
	if ts, ok := values["updated_at"]; ok {
		if v, parseErr := strconv.ParseInt(ts, 10, 64); parseErr == nil {
			meta.UpdatedAt = time.Unix(v, 0)
		}
	}
	if raw, ok := values["task_state"]; ok && raw != "" {
		var state TaskState
		if err := json.Unmarshal([]byte(raw), &state); err == nil {
			meta.TaskState = &state
		}
	}
	if raw, ok := values["entity_chain"]; ok && raw != "" {
		var chain []*EntityReference
		if err := json.Unmarshal([]byte(raw), &chain); err == nil {
			meta.EntityChain = chain
		}
	}
	return meta, nil
}

func (s *RedisStore) saveMeta(ctx context.Context, memory *WorkingMemory) error {
	fields := map[string]interface{}{
		"conversation_id": memory.ConversationID,
		"user_id":         memory.UserID,
		"current_focus":   memory.CurrentFocus,
		"created_at":      memory.CreatedAt.Unix(),
		"updated_at":      memory.UpdatedAt.Unix(),
	}
	if memory.TaskState != nil {
		raw, err := json.Marshal(memory.TaskState)
		if err != nil {
			return fmt.Errorf("marshal task state: %w", err)
		}
		fields["task_state"] = string(raw)
	} else {
		fields["task_state"] = ""
	}
	if len(memory.EntityChain) > 0 {
		raw, err := json.Marshal(memory.EntityChain)
		if err != nil {
			return fmt.Errorf("marshal entity chain: %w", err)
		}
		fields["entity_chain"] = string(raw)
	} else {
		fields["entity_chain"] = ""
	}
	if err := s.client.HSet(ctx, s.metaKey(memory.ConversationID), fields).Err(); err != nil {
		return err
	}
	s.touchTTL(ctx, memory.ConversationID)
	return nil
}

func (s *RedisStore) loadMessageList(ctx context.Context, conversationID string) ([]*repository.Message, error) {
	items, err := s.client.LRange(ctx, s.msgsKey(conversationID), 0, -1).Result()
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	msgs := make([]*repository.Message, 0, len(items))
	for _, item := range items {
		var msg repository.Message
		if err := json.Unmarshal([]byte(item), &msg); err != nil {
			return nil, fmt.Errorf("unmarshal message: %w", err)
		}
		msgs = append(msgs, &msg)
	}
	return msgs, nil
}

func (s *RedisStore) replaceMessageList(ctx context.Context, conversationID string, messages []*repository.Message) error {
	pipe := s.client.Pipeline()
	pipe.Del(ctx, s.msgsKey(conversationID))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		data, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("marshal message: %w", err)
		}
		pipe.RPush(ctx, s.msgsKey(conversationID), data)
	}
	if len(messages) > s.maxMessages {
		pipe.LTrim(ctx, s.msgsKey(conversationID), int64(-s.maxMessages), -1)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	return nil
}

func (s *RedisStore) migrateLegacyKey(ctx context.Context, conversationID string) (*WorkingMemory, error) {
	data, err := s.client.Get(ctx, s.legacyKey(conversationID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var memory WorkingMemory
	if err := json.Unmarshal(data, &memory); err != nil {
		return nil, fmt.Errorf("unmarshal legacy working memory: %w", err)
	}
	if err := s.Save(ctx, &memory); err != nil {
		return nil, err
	}
	_ = s.client.Del(ctx, s.legacyKey(conversationID)).Err()
	return &memory, nil
}
