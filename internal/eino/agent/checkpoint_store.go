package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/redis/go-redis/v9"
)

// InMemoryADKCheckPointStore 实现 Eino adk.CheckPointStore（开发/单测）。
type InMemoryADKCheckPointStore struct {
	mu sync.RWMutex
	m  map[string][]byte
}

func NewInMemoryADKCheckPointStore() *InMemoryADKCheckPointStore {
	return &InMemoryADKCheckPointStore{m: make(map[string][]byte)}
}

func (s *InMemoryADKCheckPointStore) Get(_ context.Context, checkPointID string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[checkPointID]
	return v, ok, nil
}

func (s *InMemoryADKCheckPointStore) Set(_ context.Context, checkPointID string, checkPoint []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[checkPointID] = append([]byte(nil), checkPoint...)
	return nil
}

// RedisADKCheckPointStore 实现 Eino adk.CheckPointStore（生产跨实例恢复）。
type RedisADKCheckPointStore struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

func NewRedisADKCheckPointStore(client *redis.Client, prefix string, ttl time.Duration) *RedisADKCheckPointStore {
	if prefix == "" {
		prefix = "eino:checkpoint:"
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &RedisADKCheckPointStore{client: client, prefix: prefix, ttl: ttl}
}

func (s *RedisADKCheckPointStore) key(id string) string {
	return s.prefix + id
}

func (s *RedisADKCheckPointStore) Get(ctx context.Context, checkPointID string) ([]byte, bool, error) {
	if s.client == nil {
		return nil, false, fmt.Errorf("redis client not configured")
	}
	data, err := s.client.Get(ctx, s.key(checkPointID)).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (s *RedisADKCheckPointStore) Set(ctx context.Context, checkPointID string, checkPoint []byte) error {
	if s.client == nil {
		return fmt.Errorf("redis client not configured")
	}
	return s.client.Set(ctx, s.key(checkPointID), checkPoint, s.ttl).Err()
}

// NewADKCheckPointStore 优先 Redis，否则内存。
func NewADKCheckPointStore(redisClient *redis.Client) adk.CheckPointStore {
	if redisClient != nil {
		return NewRedisADKCheckPointStore(redisClient, "", 24*time.Hour)
	}
	return NewInMemoryADKCheckPointStore()
}
