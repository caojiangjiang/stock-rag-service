package auth

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const blacklistKeyPrefix = "auth:blacklist:"

// TokenBlacklist 访问令牌撤销列表（JWT jti）。
type TokenBlacklist interface {
	Revoke(ctx context.Context, jti string, ttl time.Duration) error
	IsRevoked(ctx context.Context, jti string) (bool, error)
}

// MemoryTokenBlacklist 内存黑名单（无 Redis 时降级）。
type MemoryTokenBlacklist struct {
	mu      sync.RWMutex
	entries map[string]time.Time
}

func NewMemoryTokenBlacklist() *MemoryTokenBlacklist {
	return &MemoryTokenBlacklist{entries: make(map[string]time.Time)}
}

func (b *MemoryTokenBlacklist) Revoke(ctx context.Context, jti string, ttl time.Duration) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries[jti] = time.Now().Add(ttl)
	return nil
}

func (b *MemoryTokenBlacklist) IsRevoked(ctx context.Context, jti string) (bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	exp, ok := b.entries[jti]
	if !ok {
		return false, nil
	}
	if time.Now().After(exp) {
		return false, nil
	}
	return true, nil
}

// RedisTokenBlacklist Redis 黑名单。
type RedisTokenBlacklist struct {
	client *redis.Client
}

func NewRedisTokenBlacklist(client *redis.Client) *RedisTokenBlacklist {
	return &RedisTokenBlacklist{client: client}
}

func (b *RedisTokenBlacklist) Revoke(ctx context.Context, jti string, ttl time.Duration) error {
	if b.client == nil || jti == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return b.client.Set(ctx, blacklistKeyPrefix+jti, "1", ttl).Err()
}

func (b *RedisTokenBlacklist) IsRevoked(ctx context.Context, jti string) (bool, error) {
	if b.client == nil || jti == "" {
		return false, nil
	}
	n, err := b.client.Exists(ctx, blacklistKeyPrefix+jti).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// NewTokenBlacklist 优先 Redis，否则内存。
func NewTokenBlacklist(redisClient *redis.Client) TokenBlacklist {
	if redisClient != nil {
		return NewRedisTokenBlacklist(redisClient)
	}
	return NewMemoryTokenBlacklist()
}
