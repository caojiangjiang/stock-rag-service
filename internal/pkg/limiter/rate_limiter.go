package limiter

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter 限流器接口
type RateLimiter interface {
	// Allow 检查是否允许请求
	Allow(ctx context.Context, key string) (bool, error)
	// AllowWithLimit 检查并返回剩余额度
	AllowWithLimit(ctx context.Context, key string) (bool, int64, error)
}

// TokenBucketConfig 令牌桶配置
type TokenBucketConfig struct {
	Capacity    int64         // 桶容量
	RefillRate  int64         // 每秒补充的令牌数
	InitialRate int64         // 初始令牌数
}

// TokenBucket 令牌桶限流器（内存版本）
type TokenBucket struct {
	config  TokenBucketConfig
	buckets sync.Map
	mu      sync.Mutex
}

// NewTokenBucket 创建令牌桶限流器
func NewTokenBucket(config TokenBucketConfig) *TokenBucket {
	if config.InitialRate == 0 {
		config.InitialRate = config.Capacity
	}
	return &TokenBucket{config: config}
}

// bucket 存储每个 key 的令牌桶状态
type bucket struct {
	tokens     int64
	lastRefill time.Time
	mu         sync.Mutex
}

func (tb *TokenBucket) Allow(ctx context.Context, key string) (bool, error) {
	allowed, _, err := tb.AllowWithLimit(ctx, key)
	return allowed, err
}

func (tb *TokenBucket) AllowWithLimit(ctx context.Context, key string) (bool, int64, error) {
	b, _ := tb.buckets.LoadOrStore(key, &bucket{
		tokens:     tb.config.InitialRate,
		lastRefill: time.Now(),
	})

	bb := b.(*bucket)
	bb.mu.Lock()
	defer bb.mu.Unlock()

	// 补充令牌
	now := time.Now()
	elapsed := now.Sub(bb.lastRefill).Seconds()
	tokensToAdd := int64(elapsed * float64(tb.config.RefillRate))
	bb.tokens += tokensToAdd
	if bb.tokens > tb.config.Capacity {
		bb.tokens = tb.config.Capacity
	}
	bb.lastRefill = now

	// 检查是否有足够的令牌
	if bb.tokens > 0 {
		bb.tokens--
		return true, bb.tokens, nil
	}

	return false, 0, nil
}

// RedisTokenBucket 基于 Redis 的令牌桶限流器（分布式版本）
type RedisTokenBucket struct {
	client *redis.Client
	config TokenBucketConfig
	keyPrefix string
}

// NewRedisTokenBucket 创建基于 Redis 的分布式令牌桶限流器
func NewRedisTokenBucket(client *redis.Client, config TokenBucketConfig, keyPrefix string) *RedisTokenBucket {
	if keyPrefix == "" {
		keyPrefix = "ratelimit:"
	}
	return &RedisTokenBucket{
		client:    client,
		config:    config,
		keyPrefix: keyPrefix,
	}
}

// Allow 检查是否允许请求
func (r *RedisTokenBucket) Allow(ctx context.Context, key string) (bool, error) {
	allowed, _, err := r.AllowWithLimit(ctx, key)
	return allowed, err
}

// AllowWithLimit 检查并返回剩余额度（使用 Lua 脚本保证原子性）
func (r *RedisTokenBucket) AllowWithLimit(ctx context.Context, key string) (bool, int64, error) {
	luaScript := `
		local key = KEYS[1]
		local capacity = tonumber(ARGV[1])
		local refill_rate = tonumber(ARGV[2])
		local now = tonumber(ARGV[3])
		local requested = tonumber(ARGV[4])

		-- 获取当前状态
		local data = redis.call('HMGET', key, 'tokens', 'last_refill')
		local tokens = tonumber(data[1])
		local last_refill = tonumber(data[2])

		-- 初始化
		if tokens == nil then
			tokens = capacity
			last_refill = now
		end

		-- 计算应该补充的令牌
		local elapsed = now - last_refill
		local add_tokens = elapsed * refill_rate / 1000
		tokens = math.min(capacity, tokens + add_tokens)

		-- 更新最后补充时间
		last_refill = now

		-- 检查是否允许
		local allowed = 0
		if tokens >= requested then
			tokens = tokens - requested
			allowed = 1
		end

		-- 保存状态
		redis.call('HMSET', key, 'tokens', tokens, 'last_refill', last_refill)
		redis.call('EXPIRE', key, 3600)

		return {allowed, math.floor(tokens)}
	`

	fullKey := r.keyPrefix + key
	now := time.Now().UnixMilli()

	result, err := r.client.Eval(ctx, luaScript, []string{fullKey},
		r.config.Capacity,
		r.config.RefillRate,
		now,
		1,
	).Result()

	if err != nil {
		return false, 0, fmt.Errorf("redis eval failed: %w", err)
	}

	results, ok := result.([]interface{})
	if !ok || len(results) < 2 {
		return false, 0, fmt.Errorf("unexpected redis result")
	}

	allowed := results[0].(int64) == 1
	remaining := results[1].(int64)

	return allowed, remaining, nil
}

// RateLimitMiddleware 限流中间件
func RateLimitMiddleware(limiter RateLimiter, keyFunc func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFunc(r)

			allowed, remaining, err := limiter.AllowWithLimit(r.Context(), key)
			if err != nil {
				// 如果限流器出错，拒绝请求
				http.Error(w, "Rate limiter error", http.StatusServiceUnavailable)
				return
			}

			// 设置限流相关的响应头
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))

			if !allowed {
				w.Header().Set("X-RateLimit-Retry-After", "1")
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// DefaultKeyFunc 默认的限流 key 函数（按 IP）
func DefaultKeyFunc(r *http.Request) string {
	return r.RemoteAddr
}

// UserKeyFunc 按用户 ID 限流（需要认证）
func UserKeyFunc(r *http.Request) string {
	// 从上下文中获取用户 ID（需要在认证中间件中设置）
	if userID, ok := r.Context().Value("user_id").(string); ok {
		return "user:" + userID
	}
	// 如果没有用户 ID，使用 IP
	return "ip:" + r.RemoteAddr
}
