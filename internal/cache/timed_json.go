package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// TimedJSONStore 带 cached_at 的 JSON  blob 存储（Redis TTL 控制过期）。
type TimedJSONStore struct {
	client *redis.Client
	prefix string
}

func NewTimedJSONStore(client *redis.Client, prefix string) *TimedJSONStore {
	if client == nil || prefix == "" {
		return nil
	}
	return &TimedJSONStore{client: client, prefix: prefix}
}

func (s *TimedJSONStore) Enabled() bool {
	return s != nil && s.client != nil
}

func (s *TimedJSONStore) Get(ctx context.Context, key string, dest any) (cachedAt time.Time, ok bool, err error) {
	if !s.Enabled() {
		return time.Time{}, false, nil
	}
	raw, err := s.client.Get(ctx, s.fullKey(key)).Bytes()
	if err == redis.Nil {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	var wrap struct {
		CachedAt time.Time       `json:"cached_at"`
		Data     json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return time.Time{}, false, fmt.Errorf("decode cache wrapper: %w", err)
	}
	if err := json.Unmarshal(wrap.Data, dest); err != nil {
		return time.Time{}, false, fmt.Errorf("decode cache data: %w", err)
	}
	if wrap.CachedAt.IsZero() {
		wrap.CachedAt = time.Now()
	}
	return wrap.CachedAt, true, nil
}

func (s *TimedJSONStore) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if !s.Enabled() {
		return nil
	}
	wrap := struct {
		CachedAt time.Time `json:"cached_at"`
		Data     any       `json:"data"`
	}{
		CachedAt: time.Now(),
		Data:     value,
	}
	b, err := json.Marshal(wrap)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, s.fullKey(key), b, ttl).Err()
}

func (s *TimedJSONStore) Delete(ctx context.Context, key string) error {
	if !s.Enabled() {
		return nil
	}
	return s.client.Del(ctx, s.fullKey(key)).Err()
}

func (s *TimedJSONStore) Exists(ctx context.Context, key string) (bool, error) {
	if !s.Enabled() {
		return false, nil
	}
	n, err := s.client.Exists(ctx, s.fullKey(key)).Result()
	return n > 0, err
}

func (s *TimedJSONStore) fullKey(key string) string {
	return s.prefix + ":" + key
}
