package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const refreshKeyPrefix = "auth:refresh:"

// RefreshRecord refresh token 关联的会话信息。
type RefreshRecord struct {
	UserID    string    `json:"user_id"`
	SessionID string    `json:"session_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// RefreshStore 存储 opaque refresh token。
type RefreshStore interface {
	Save(ctx context.Context, token string, record RefreshRecord, ttl time.Duration) error
	Get(ctx context.Context, token string) (*RefreshRecord, bool)
	Delete(ctx context.Context, token string) error
	DeleteBySession(ctx context.Context, sessionID string) error
}

func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// MemoryRefreshStore 内存 refresh 存储。
type MemoryRefreshStore struct {
	mu       sync.RWMutex
	byToken  map[string]RefreshRecord
	bySession map[string]string
}

func NewMemoryRefreshStore() *MemoryRefreshStore {
	return &MemoryRefreshStore{
		byToken:   make(map[string]RefreshRecord),
		bySession: make(map[string]string),
	}
}

func (s *MemoryRefreshStore) Save(ctx context.Context, token string, record RefreshRecord, ttl time.Duration) error {
	key := hashRefreshToken(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.bySession[record.SessionID]; ok {
		delete(s.byToken, old)
	}
	s.byToken[key] = record
	s.bySession[record.SessionID] = key
	return nil
}

func (s *MemoryRefreshStore) Get(ctx context.Context, token string) (*RefreshRecord, bool) {
	key := hashRefreshToken(token)
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.byToken[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(record.ExpiresAt) {
		return nil, false
	}
	return &record, true
}

func (s *MemoryRefreshStore) Delete(ctx context.Context, token string) error {
	key := hashRefreshToken(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.byToken[key]
	if ok {
		delete(s.bySession, record.SessionID)
	}
	delete(s.byToken, key)
	return nil
}

func (s *MemoryRefreshStore) DeleteBySession(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if key, ok := s.bySession[sessionID]; ok {
		delete(s.byToken, key)
		delete(s.bySession, sessionID)
	}
	return nil
}

// RedisRefreshStore Redis refresh 存储。
type RedisRefreshStore struct {
	client *redis.Client
}

func NewRedisRefreshStore(client *redis.Client) *RedisRefreshStore {
	return &RedisRefreshStore{client: client}
}

func (s *RedisRefreshStore) Save(ctx context.Context, token string, record RefreshRecord, ttl time.Duration) error {
	if s.client == nil {
		return nil
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	key := refreshKeyPrefix + hashRefreshToken(token)
	pipe := s.client.Pipeline()
	pipe.Set(ctx, key, data, ttl)
	pipe.Set(ctx, refreshKeyPrefix+"session:"+record.SessionID, hashRefreshToken(token), ttl)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisRefreshStore) Get(ctx context.Context, token string) (*RefreshRecord, bool) {
	if s.client == nil {
		return nil, false
	}
	data, err := s.client.Get(ctx, refreshKeyPrefix+hashRefreshToken(token)).Bytes()
	if err != nil {
		return nil, false
	}
	var record RefreshRecord
	if json.Unmarshal(data, &record) != nil {
		return nil, false
	}
	if time.Now().After(record.ExpiresAt) {
		return nil, false
	}
	return &record, true
}

func (s *RedisRefreshStore) Delete(ctx context.Context, token string) error {
	if s.client == nil {
		return nil
	}
	key := refreshKeyPrefix + hashRefreshToken(token)
	recordData, err := s.client.Get(ctx, key).Bytes()
	if err == nil {
		var record RefreshRecord
		if json.Unmarshal(recordData, &record) == nil {
			s.client.Del(ctx, refreshKeyPrefix+"session:"+record.SessionID)
		}
	}
	return s.client.Del(ctx, key).Err()
}

func (s *RedisRefreshStore) DeleteBySession(ctx context.Context, sessionID string) error {
	if s.client == nil {
		return nil
	}
	tokenHash, err := s.client.Get(ctx, refreshKeyPrefix+"session:"+sessionID).Result()
	if err != nil {
		return nil
	}
	pipe := s.client.Pipeline()
	pipe.Del(ctx, refreshKeyPrefix+tokenHash)
	pipe.Del(ctx, refreshKeyPrefix+"session:"+sessionID)
	_, err = pipe.Exec(ctx)
	return err
}

// NewRefreshStore 优先 Redis，否则内存。
func NewRefreshStore(redisClient *redis.Client) RefreshStore {
	if redisClient != nil {
		return NewRedisRefreshStore(redisClient)
	}
	return NewMemoryRefreshStore()
}
