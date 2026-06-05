package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// InterruptSessionStore 保存 HITL 恢复所需的业务元数据。
type InterruptSessionStore interface {
	Save(ctx context.Context, session *InterruptSession) error
	Get(ctx context.Context, checkPointID string) (*InterruptSession, error)
	Delete(ctx context.Context, checkPointID string) error
}

type InMemoryInterruptSessionStore struct {
	mu sync.RWMutex
	m  map[string]*InterruptSession
}

func NewInMemoryInterruptSessionStore() *InMemoryInterruptSessionStore {
	return &InMemoryInterruptSessionStore{m: make(map[string]*InterruptSession)}
}

func (s *InMemoryInterruptSessionStore) Save(_ context.Context, session *InterruptSession) error {
	if session == nil || session.CheckPointID == "" {
		return fmt.Errorf("invalid interrupt session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	copied := *session
	s.m[session.CheckPointID] = &copied
	return nil
}

func (s *InMemoryInterruptSessionStore) Get(_ context.Context, checkPointID string) (*InterruptSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.m[checkPointID]
	if !ok {
		return nil, fmt.Errorf("interrupt session %s not found", checkPointID)
	}
	copied := *session
	return &copied, nil
}

func (s *InMemoryInterruptSessionStore) Delete(_ context.Context, checkPointID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, checkPointID)
	return nil
}

type RedisInterruptSessionStore struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

func NewRedisInterruptSessionStore(client *redis.Client, prefix string, ttl time.Duration) *RedisInterruptSessionStore {
	if prefix == "" {
		prefix = "hitl:session:"
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &RedisInterruptSessionStore{client: client, prefix: prefix, ttl: ttl}
}

func (s *RedisInterruptSessionStore) key(id string) string {
	return s.prefix + id
}

func (s *RedisInterruptSessionStore) Save(ctx context.Context, session *InterruptSession) error {
	if s.client == nil {
		return fmt.Errorf("redis client not configured")
	}
	if session == nil || session.CheckPointID == "" {
		return fmt.Errorf("invalid interrupt session")
	}
	b, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, s.key(session.CheckPointID), b, s.ttl).Err()
}

func (s *RedisInterruptSessionStore) Get(ctx context.Context, checkPointID string) (*InterruptSession, error) {
	if s.client == nil {
		return nil, fmt.Errorf("redis client not configured")
	}
	data, err := s.client.Get(ctx, s.key(checkPointID)).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("interrupt session %s not found", checkPointID)
	}
	if err != nil {
		return nil, err
	}
	var session InterruptSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *RedisInterruptSessionStore) Delete(ctx context.Context, checkPointID string) error {
	if s.client == nil {
		return fmt.Errorf("redis client not configured")
	}
	return s.client.Del(ctx, s.key(checkPointID)).Err()
}

func NewInterruptSessionStore(redisClient *redis.Client) InterruptSessionStore {
	if redisClient != nil {
		return NewRedisInterruptSessionStore(redisClient, "", 24*time.Hour)
	}
	return NewInMemoryInterruptSessionStore()
}
