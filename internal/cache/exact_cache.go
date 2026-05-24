package cache

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"

	"stock_rag/internal/observability"
)

// ExactCacheResult 精确缓存命中结果
type ExactCacheResult struct {
	Response     string
	ResponseAt   time.Time
	StockCode    string
	TopK         int
	AccessCount  int
	Hit          bool
}

// ExactCacheConfig 精确缓存配置
type ExactCacheConfig struct {
	TTL          time.Duration // 缓存过期时间（默认24小时）
	MaxCacheSize int           // 最大缓存条数（默认10000）
}

// DefaultExactCacheConfig 返回默认配置
func DefaultExactCacheConfig() ExactCacheConfig {
	return ExactCacheConfig{
		TTL:          24 * time.Hour,
		MaxCacheSize: 10000,
	}
}

// ExactCacheEntry 精确缓存条目
type ExactCacheEntry struct {
	CacheKey    string `json:"cache_key"`
	Response    string `json:"response"`
	CachedAt    int64  `json:"cached_at"`
	AccessedAt  int64  `json:"accessed_at"`
	AccessCount int    `json:"access_count"`
}

// ExactCache 基于 Redis 的精确缓存实现
// 用于缓存用户原始问题的查询结果，基于复合键的 MD5 哈希进行精确匹配
type ExactCache struct {
	client       *redis.Client
	ttl          time.Duration
	maxCacheSize int
}

// Redis 键前缀（与语义缓存区分）
const (
	RedisKeyExactPrefix = "exact_cache:"    // 精确缓存键前缀
	RedisKeyExactSet    = "exact_cache:idx" // 精确缓存索引集合
)

// NewExactCache 创建精确缓存
func NewExactCache(client *redis.Client, config ExactCacheConfig) *ExactCache {
	if config.TTL == 0 {
		config.TTL = DefaultExactCacheConfig().TTL
	}
	if config.MaxCacheSize == 0 {
		config.MaxCacheSize = DefaultExactCacheConfig().MaxCacheSize
	}

	return &ExactCache{
		client:       client,
		ttl:          config.TTL,
		maxCacheSize: config.MaxCacheSize,
	}
}

// Get 获取精确缓存
// cacheKey 是由 buildExactCacheKey 生成的复合键
func (c *ExactCache) Get(ctx context.Context, cacheKey string) (*ExactCacheResult, error) {
	key := RedisKeyExactPrefix + c.hashKey(cacheKey)

	// 获取缓存数据
	data, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return &ExactCacheResult{Hit: false}, nil
	}
	if err != nil {
		observability.L().WarnCtx(ctx, "ExactCache get failed", "error", err)
		return nil, err
	}

	var entry ExactCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}

	// 检查是否过期
	cachedAt := time.Unix(entry.CachedAt, 0)
	if time.Since(cachedAt) > c.ttl {
		// 过期，异步删除
		go func() {
			c.client.Del(context.Background(), key)
			c.client.SRem(context.Background(), RedisKeyExactSet, c.hashKey(cacheKey))
		}()
		return &ExactCacheResult{Hit: false}, nil
	}

	// 更新访问信息
	entry.AccessedAt = time.Now().Unix()
	entry.AccessCount++

	newData, _ := json.Marshal(entry)
	c.client.Set(ctx, key, newData, c.ttl)

	observability.L().InfoCtx(ctx, "ExactCache hit",
		"cache_key", truncateString(cacheKey, 50),
		"access_count", entry.AccessCount,
	)

	return &ExactCacheResult{
		Response:    entry.Response,
		ResponseAt:  cachedAt,
		AccessCount: entry.AccessCount,
		Hit:         true,
	}, nil
}

// Set 设置精确缓存
func (c *ExactCache) Set(ctx context.Context, cacheKey, response string) error {
	key := RedisKeyExactPrefix + c.hashKey(cacheKey)

	now := time.Now().Unix()

	entry := ExactCacheEntry{
		CacheKey:    cacheKey,
		Response:    response,
		CachedAt:    now,
		AccessedAt:  now,
		AccessCount: 0,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	// 使用 Pipeline 批量写入
	pipe := c.client.Pipeline()
	pipe.Set(ctx, key, data, c.ttl)
	pipe.SAdd(ctx, RedisKeyExactSet, c.hashKey(cacheKey))
	_, err = pipe.Exec(ctx)

	if err != nil {
		observability.L().WarnCtx(ctx, "ExactCache set failed", "error", err)
		return err
	}

	// 检查缓存大小，必要时清理
	go func() {
		if err := c.checkAndEvict(context.Background()); err != nil {
			observability.L().WarnCtx(context.Background(), "ExactCache evict warning", "error", err)
		}
	}()

	observability.L().InfoCtx(ctx, "ExactCache entry added",
		"cache_key", truncateString(cacheKey, 50),
		"ttl", c.ttl.String(),
	)

	return nil
}

// Delete 删除缓存条目
func (c *ExactCache) Delete(ctx context.Context, cacheKey string) error {
	key := RedisKeyExactPrefix + c.hashKey(cacheKey)

	pipe := c.client.Pipeline()
	pipe.Del(ctx, key)
	pipe.SRem(ctx, RedisKeyExactSet, c.hashKey(cacheKey))
	_, err := pipe.Exec(ctx)

	if err != nil {
		observability.L().WarnCtx(ctx, "ExactCache delete warning", "error", err)
	}

	observability.L().InfoCtx(ctx, "ExactCache entry deleted", "cache_key", cacheKey)
	return nil
}

// Clear 清空所有精确缓存
func (c *ExactCache) Clear(ctx context.Context) error {
	var cursor uint64
	var keys []string

	for {
		var err error
		var batch []string
		batch, cursor, err = c.client.Scan(ctx, cursor, RedisKeyExactPrefix+"*", 100).Result()
		if err != nil {
			return err
		}
		keys = append(keys, batch...)

		if cursor == 0 {
			break
		}
	}

	// 添加索引集合
	keys = append(keys, RedisKeyExactSet)

	if len(keys) > 0 {
		if err := c.client.Del(ctx, keys...).Err(); err != nil {
			return err
		}
	}

	observability.L().InfoCtx(ctx, "ExactCache cleared", "deleted_keys", len(keys))
	return nil
}

// Stats 返回缓存统计信息
func (c *ExactCache) Stats() (stats ExactCacheStats) {
	stats.TotalEntries = c.maxCacheSize
	stats.TTLSeconds = int(c.ttl.Seconds())

	ctx := context.Background()
	hashes, err := c.client.SMembers(ctx, RedisKeyExactSet).Result()
	if err != nil {
		observability.L().WarnCtx(ctx, "ExactCache Stats failed", "error", err)
		return stats
	}

	var total, valid int

	for _, hash := range hashes {
		total++

		key := RedisKeyExactPrefix + hash
		data, err := c.client.Get(ctx, key).Bytes()
		if err != nil {
			continue
		}

		var entry ExactCacheEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}

		cachedAt := time.Unix(entry.CachedAt, 0)
		if time.Since(cachedAt) <= c.ttl {
			valid++
			stats.TotalAccessCount += entry.AccessCount
		}
	}

	stats.TotalEntries = total
	stats.ValidEntries = valid
	stats.ExpiredEntries = total - valid

	if valid > 0 {
		stats.AverageAccessCount = stats.TotalAccessCount / valid
	}

	return stats
}

// ExactCacheStats 精确缓存统计
type ExactCacheStats struct {
	TotalEntries       int
	ValidEntries       int
	ExpiredEntries     int
	MaxEntries         int
	TTLSeconds         int
	TotalAccessCount   int
	AverageAccessCount int
}

// hashKey 对缓存键进行哈希
func (c *ExactCache) hashKey(key string) string {
	hash := md5.Sum([]byte(key))
	return hex.EncodeToString(hash[:])
}

// checkAndEvict 检查并清理缓存大小
func (c *ExactCache) checkAndEvict(ctx context.Context) error {
	stats := c.Stats()
	if stats.ValidEntries < c.maxCacheSize {
		return nil
	}

	hashes, err := c.client.SMembers(ctx, RedisKeyExactSet).Result()
	if err != nil {
		return err
	}

	type entryInfo struct {
		hash       string
		accessedAt int64
		count      int
	}
	var allEntries []entryInfo

	for _, hash := range hashes {
		key := RedisKeyExactPrefix + hash
		data, err := c.client.Get(ctx, key).Bytes()
		if err != nil {
			continue
		}

		var entry ExactCacheEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}

		allEntries = append(allEntries, entryInfo{
			hash:      hash,
			accessedAt: entry.AccessedAt,
			count:     entry.AccessCount,
		})
	}

	if len(allEntries) == 0 {
		return nil
	}

	// 按访问次数和时间排序
	// 淘汰访问次数少且久未访问的
	for i := 0; i < len(allEntries); i++ {
		for j := i + 1; j < len(allEntries); j++ {
			if allEntries[i].count > allEntries[j].count {
				allEntries[i], allEntries[j] = allEntries[j], allEntries[i]
			} else if allEntries[i].count == allEntries[j].count {
				if allEntries[i].accessedAt > allEntries[j].accessedAt {
					allEntries[i], allEntries[j] = allEntries[j], allEntries[i]
				}
			}
		}
	}

	// 删除访问次数最少的条目
	deleteCount := len(allEntries) - c.maxCacheSize/2
	if deleteCount > 0 {
		for i := 0; i < deleteCount; i++ {
			hash := allEntries[i].hash

			pipe := c.client.Pipeline()
			pipe.Del(ctx, RedisKeyExactPrefix+hash)
			pipe.SRem(ctx, RedisKeyExactSet, hash)
			pipe.Exec(ctx)
		}

		observability.L().InfoCtx(ctx, "ExactCache evicted entries", "count", deleteCount)
	}

	return nil
}

// truncateString 截断字符串
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// RedisClient 获取 Redis 客户端
func (c *ExactCache) RedisClient() *redis.Client {
	return c.client
}
