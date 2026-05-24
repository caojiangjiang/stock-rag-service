package cache

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"

	"stock_rag/internal/embedding"
	"stock_rag/internal/observability"
)

// RedisCacheKeyPrefix Redis 键前缀
// 分离存储优化：向量、响应、元数据分开存储
const (
	RedisKeyPrefix   = "semantic_cache:"
	RedisKeyResponse = RedisKeyPrefix + "resp:" // 响应内容存储
	RedisKeyVector   = RedisKeyPrefix + "vec:"  // 向量数据存储
	RedisKeyMetadata = RedisKeyPrefix + "meta:" // 元数据存储
	RedisKeyIndexSet = RedisKeyPrefix + "idx"   // 索引集合（记录所有缓存的 hash）
)

// CacheResult 缓存命中结果
type CacheResult struct {
	Content     string
	Score       float64
	CachedAt    time.Time
	Hit         bool
	Source      string // 命中来源：redis
	AccessCount int
}

// SemanticCacheConfig 语义缓存配置
type SemanticCacheConfig struct {
	Embedder            embedding.Embedder // 向量化器
	SimilarityThreshold float64            // 相似度阈值（默认0.85）
	TTL                 time.Duration      // 缓存过期时间（默认1小时）
	MaxCacheSize        int                // 最大缓存条数（默认1000）
	EnableExactMatch    bool               // 是否同时启用精确匹配
	BatchSize           int                // 批量搜索时获取的候选数量（默认100）
}

// DefaultSemanticCacheConfig 返回默认配置
func DefaultSemanticCacheConfig() SemanticCacheConfig {
	return SemanticCacheConfig{
		SimilarityThreshold: 0.85,
		TTL:                 1 * time.Hour,
		MaxCacheSize:        1000,
		EnableExactMatch:    true,
		BatchSize:           100,
	}
}

// RedisCacheEntry Redis 缓存条目
type RedisCacheEntry struct {
	Query       string    `json:"query"`
	Response    string    `json:"response"`
	Vector      []float32 `json:"vector"`
	CachedAt    int64     `json:"cached_at"`
	AccessedAt  int64     `json:"accessed_at"`
	AccessCount int       `json:"access_count"`
}

// RedisSemanticCache 基于 Redis 的语义缓存实现
// 所有缓存数据（包括向量）都存储在 Redis 中
type RedisSemanticCache struct {
	client              *redis.Client
	embedder            embedding.Embedder
	similarityThreshold float64
	ttl                 time.Duration
	maxCacheSize        int
	enableExactMatch    bool
	batchSize           int
}

// NewRedisSemanticCache 创建基于 Redis 的语义缓存
func NewRedisSemanticCache(client *redis.Client, embedder embedding.Embedder, config SemanticCacheConfig) *RedisSemanticCache {
	if config.SimilarityThreshold == 0 {
		config.SimilarityThreshold = DefaultSemanticCacheConfig().SimilarityThreshold
	}
	if config.TTL == 0 {
		config.TTL = DefaultSemanticCacheConfig().TTL
	}
	if config.MaxCacheSize == 0 {
		config.MaxCacheSize = DefaultSemanticCacheConfig().MaxCacheSize
	}
	if config.BatchSize == 0 {
		config.BatchSize = DefaultSemanticCacheConfig().BatchSize
	}

	return &RedisSemanticCache{
		client:              client,
		embedder:            embedder,
		similarityThreshold: config.SimilarityThreshold,
		ttl:                 config.TTL,
		maxCacheSize:        config.MaxCacheSize,
		enableExactMatch:    config.EnableExactMatch,
		batchSize:           config.BatchSize,
	}
}

// Get 语义缓存查找
// 优先精确匹配，然后基于 embedding 相似度匹配
func (c *RedisSemanticCache) Get(ctx context.Context, query string) (*CacheResult, error) {
	observability.L().DebugCtx(ctx, "RedisSemanticCache.Get called", "query", query)

	// 1. 精确匹配
	if c.enableExactMatch {
		result, err := c.getExactMatch(ctx, query)
		if err != nil {
			observability.L().WarnCtx(ctx, "RedisSemanticCache exact match error", "error", err)
		}
		if result != nil && result.Hit {
			return result, nil
		}
	}

	// 2. 语义相似度匹配
	return c.getSemanticMatch(ctx, query)
}

// getExactMatch 精确匹配（分离存储读取）
func (c *RedisSemanticCache) getExactMatch(ctx context.Context, query string) (*CacheResult, error) {
	queryHash := hashQuery(query)
	key := RedisKeyResponse + queryHash
	metaKey := RedisKeyMetadata + queryHash

	// 使用 Pipeline 批量获取
	pipe := c.client.Pipeline()
	respPipe := pipe.Get(ctx, key)
	metaPipe := pipe.Get(ctx, metaKey)
	_, _ = pipe.Exec(ctx) // 忽略错误，让后续处理

	// 获取响应内容
	response, err := respPipe.Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// 获取元数据
	metaData, err := metaPipe.Result()
	if err != nil && err != redis.Nil {
		observability.L().WarnCtx(ctx, "RedisSemanticCache get metadata failed", "error", err)
	}

	var metadata map[string]interface{}
	if err == nil {
		json.Unmarshal([]byte(metaData), &metadata)
	}

	// 检查是否过期
	cachedAtUnix := int64(0)
	if cachedAt, ok := metadata["cached_at"].(float64); ok {
		cachedAtUnix = int64(cachedAt)
	}
	cachedAt := time.Unix(cachedAtUnix, 0)

	if cachedAtUnix > 0 && time.Since(cachedAt) > c.ttl {
		// 过期，异步删除
		go func() {
			c.client.Del(context.Background(), key, metaKey, RedisKeyVector+queryHash)
		}()
		return nil, nil
	}

	// 更新访问信息
	accessCount := 0
	if count, ok := metadata["access_count"].(float64); ok {
		accessCount = int(count)
	}
	accessCount++

	metadata["accessed_at"] = time.Now().Unix()
	metadata["access_count"] = accessCount
	newMetaData, _ := json.Marshal(metadata)
	c.client.Set(ctx, metaKey, newMetaData, c.ttl)

	observability.L().InfoCtx(ctx, "RedisSemanticCache exact hit",
		"query", query,
		"access_count", accessCount,
	)

	return &CacheResult{
		Content:     response,
		Score:       1.0,
		CachedAt:    cachedAt,
		Hit:         true,
		Source:      "redis_exact",
		AccessCount: accessCount,
	}, nil
}

// SemanticCandidate 语义候选条目
type SemanticCandidate struct {
	Query       string
	Response    string
	Vector      []float32
	CachedAt    int64
	AccessCount int
}

// getSemanticMatch 语义相似度匹配（分离存储读取）
func (c *RedisSemanticCache) getSemanticMatch(ctx context.Context, query string) (*CacheResult, error) {
	// 生成查询向量
	queryVector, err := c.embedder.EmbedQuery(ctx, query)
	if err != nil {
		observability.L().WarnCtx(ctx, "RedisSemanticCache embed query failed", "error", err)
		return nil, err
	}

	// 获取所有缓存向量进行相似度计算
	candidates, err := c.getAllCandidates(ctx)
	if err != nil {
		observability.L().WarnCtx(ctx, "RedisSemanticCache get candidates failed", "error", err)
		return nil, err
	}

	if len(candidates) == 0 {
		observability.L().DebugCtx(ctx, "RedisSemanticCache no candidates", "query", query)
		return &CacheResult{Hit: false}, nil
	}

	// 计算相似度并排序
	type scoredEntry struct {
		entry SemanticCandidate
		score float64
	}
	scored := make([]scoredEntry, 0, len(candidates))

	for _, entry := range candidates {
		if entry.Vector == nil {
			continue
		}
		score := cosineSimilarity(queryVector, entry.Vector)
		if score > 0 { // 只保留有相似度的
			scored = append(scored, scoredEntry{entry: entry, score: score})
		}
	}

	if len(scored) == 0 {
		return &CacheResult{Hit: false}, nil
	}

	// 按相似度降序排序
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// 检查最高相似度是否满足阈值
	best := scored[0]
	if best.score < c.similarityThreshold {
		observability.L().DebugCtx(ctx, "RedisSemanticCache semantic miss",
			"query", query,
			"best_score", best.score,
			"threshold", c.similarityThreshold,
		)
		return &CacheResult{Hit: false}, nil
	}

	// 更新访问信息
	best.entry.AccessCount++
	queryHash := hashQuery(best.entry.Query)

	// 获取现有元数据并更新
	metaKey := RedisKeyMetadata + queryHash
	metaData, _ := c.client.Get(ctx, metaKey).Bytes()
	var metadata map[string]interface{}
	if len(metaData) > 0 {
		json.Unmarshal(metaData, &metadata)
	}
	metadata["accessed_at"] = time.Now().Unix()
	metadata["access_count"] = best.entry.AccessCount
	newMetaData, _ := json.Marshal(metadata)
	c.client.Set(ctx, metaKey, newMetaData, c.ttl)

	observability.L().InfoCtx(ctx, "RedisSemanticCache semantic hit",
		"query", query,
		"cached_query", best.entry.Query,
		"similarity", best.score,
		"threshold", c.similarityThreshold,
		"access_count", best.entry.AccessCount,
	)

	return &CacheResult{
		Content:     best.entry.Response,
		Score:       best.score,
		CachedAt:    time.Unix(best.entry.CachedAt, 0),
		Hit:         true,
		Source:      "redis_semantic",
		AccessCount: best.entry.AccessCount,
	}, nil
}

// Set 缓存查询结果
func (c *RedisSemanticCache) Set(ctx context.Context, query, response string) error {
	observability.L().DebugCtx(ctx, "RedisSemanticCache.Set called", "query", query)

	// 生成查询向量
	queryVector, err := c.embedder.EmbedQuery(ctx, query)
	if err != nil {
		observability.L().WarnCtx(ctx, "RedisSemanticCache embed query failed", "error", err)
		return err
	}

	// 检查缓存大小，必要时清理
	if err := c.checkAndEvict(ctx); err != nil {
		observability.L().WarnCtx(ctx, "RedisSemanticCache evict warning", "error", err)
	}

	queryHash := hashQuery(query)
	cachedAt := time.Now().Unix()

	// 分离存储优化：使用 Pipeline 批量写入
	pipe := c.client.Pipeline()

	// 1. 存储响应内容（分离存储）
	pipe.Set(ctx, RedisKeyResponse+queryHash, response, c.ttl)

	// 2. 存储向量数据（分离存储）
	vectorData, _ := json.Marshal(queryVector)
	pipe.Set(ctx, RedisKeyVector+queryHash, vectorData, c.ttl)

	// 3. 存储元数据
	metadata := map[string]interface{}{
		"query_hash":   queryHash,
		"query":        query,
		"cached_at":    cachedAt,
		"accessed_at":  cachedAt,
		"access_count": 0,
	}
	metadataData, _ := json.Marshal(metadata)
	pipe.Set(ctx, RedisKeyMetadata+queryHash, metadataData, c.ttl)

	// 4. 添加到索引集合（用于遍历查找）
	pipe.SAdd(ctx, RedisKeyIndexSet, queryHash)

	// 执行批量写入
	_, err = pipe.Exec(ctx)
	if err != nil {
		observability.L().WarnCtx(ctx, "RedisSemanticCache Set failed", "error", err)
		return err
	}

	observability.L().InfoCtx(ctx, "RedisSemanticCache entry added",
		"query", query,
		"query_hash", queryHash,
		"ttl", c.ttl.String(),
	)

	return nil
}

// Delete 删除缓存条目
func (c *RedisSemanticCache) Delete(ctx context.Context, query string) error {
	queryHash := hashQuery(query)

	pipe := c.client.Pipeline()
	pipe.Del(ctx, RedisKeyResponse+queryHash)
	pipe.Del(ctx, RedisKeyVector+queryHash)
	pipe.Del(ctx, RedisKeyMetadata+queryHash)
	pipe.SRem(ctx, RedisKeyIndexSet, queryHash) // 从索引集合中删除
	_, err := pipe.Exec(ctx)

	if err != nil {
		observability.L().WarnCtx(ctx, "RedisSemanticCache delete warning", "error", err)
	}

	observability.L().InfoCtx(ctx, "RedisSemanticCache entry deleted", "query", query, "hash", queryHash)
	return nil
}

// Clear 清空所有缓存
func (c *RedisSemanticCache) Clear(ctx context.Context) error {
	// 使用 SCAN 查找并删除所有相关键
	var cursor uint64
	var keys []string

	for {
		var err error
		var batch []string
		batch, cursor, err = c.client.Scan(ctx, cursor, RedisKeyPrefix+"*", 100).Result()
		if err != nil {
			return err
		}
		keys = append(keys, batch...)

		if cursor == 0 {
			break
		}
	}

	// 添加索引集合
	keys = append(keys, RedisKeyIndexSet)

	if len(keys) > 0 {
		if err := c.client.Del(ctx, keys...).Err(); err != nil {
			return err
		}
	}

	observability.L().InfoCtx(ctx, "RedisSemanticCache cleared", "deleted_keys", len(keys))
	return nil
}

// CleanExpired 清理过期缓存
func (c *RedisSemanticCache) CleanExpired(ctx context.Context) (int, error) {
	var cursor uint64
	var count int
	now := time.Now().Unix()

	for {
		var err error
		var keys []string
		keys, cursor, err = c.client.Scan(ctx, cursor, RedisKeyMetadata+":*", 100).Result()
		if err != nil {
			return count, err
		}

		for _, key := range keys {
			data, err := c.client.Get(ctx, key).Bytes()
			if err == redis.Nil {
				continue
			}
			if err != nil {
				continue
			}

			var metadata map[string]interface{}
			if err := json.Unmarshal(data, &metadata); err != nil {
				continue
			}

			cachedAt, ok := metadata["cached_at"].(float64)
			if !ok {
				continue
			}

			if now-int64(cachedAt) > int64(c.ttl.Seconds()) {
				// 过期，删除
				queryHash := metadata["query_hash"].(string)
				c.client.Del(ctx, RedisKeyResponse+queryHash)
				c.client.Del(ctx, RedisKeyVector+queryHash)
				c.client.Del(ctx, RedisKeyMetadata+queryHash)
				c.client.Del(ctx, RedisKeyMetadata+queryHash)
				count++
			}
		}

		if cursor == 0 {
			break
		}
	}

	if count > 0 {
		observability.L().InfoCtx(ctx, "RedisSemanticCache expired entries cleaned", "count", count)
	}

	return count, nil
}

// Stats 返回缓存统计信息（基于索引集合）
func (c *RedisSemanticCache) Stats() (stats CacheStats) {
	stats.TotalEntries = c.maxCacheSize
	stats.SimilarityThreshold = c.similarityThreshold
	stats.TTLSeconds = int(c.ttl.Seconds())

	// 从索引集合中获取所有 hash
	ctx := context.Background()
	hashes, err := c.client.SMembers(ctx, RedisKeyIndexSet).Result()
	if err != nil {
		observability.L().WarnCtx(ctx, "RedisSemanticCache Stats get index set failed", "error", err)
		return stats
	}

	var total, valid int

	// 统计每个缓存条目的状态
	for _, queryHash := range hashes {
		total++

		// 获取元数据
		metaKey := RedisKeyMetadata + queryHash
		metaData, err := c.client.Get(ctx, metaKey).Bytes()
		if err != nil {
			continue
		}

		var metadata map[string]interface{}
		if err := json.Unmarshal(metaData, &metadata); err != nil {
			continue
		}

		// 检查是否过期
		cachedAt := int64(0)
		if cachedAtVal, ok := metadata["cached_at"].(float64); ok {
			cachedAt = int64(cachedAtVal)
		}

		if cachedAt > 0 && time.Since(time.Unix(cachedAt, 0)) <= c.ttl {
			valid++
			if count, ok := metadata["access_count"].(float64); ok {
				stats.TotalAccessCount += int(count)
			}
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

// getAllCandidates 获取所有缓存条目用于相似度计算（分离存储读取）
func (c *RedisSemanticCache) getAllCandidates(ctx context.Context) ([]SemanticCandidate, error) {
	// 从索引集合中获取所有缓存的 hash
	hashes, err := c.client.SMembers(ctx, RedisKeyIndexSet).Result()
	if err != nil {
		observability.L().WarnCtx(ctx, "RedisSemanticCache get index set failed", "error", err)
		return nil, err
	}

	if len(hashes) == 0 {
		return nil, nil
	}

	// 限制候选数量
	if len(hashes) > c.batchSize {
		hashes = hashes[:c.batchSize]
	}

	var candidates []SemanticCandidate

	// 批量获取向量数据
	for _, queryHash := range hashes {
		// 1. 获取向量
		vectorKey := RedisKeyVector + queryHash
		vectorData, err := c.client.Get(ctx, vectorKey).Bytes()
		if err != nil {
			continue
		}

		var vector []float32
		if err := json.Unmarshal(vectorData, &vector); err != nil {
			continue
		}

		// 2. 获取元数据
		metaKey := RedisKeyMetadata + queryHash
		metaData, err := c.client.Get(ctx, metaKey).Bytes()
		if err != nil {
			continue
		}

		var metadata map[string]interface{}
		if err := json.Unmarshal(metaData, &metadata); err != nil {
			continue
		}

		// 检查是否过期
		cachedAt := int64(0)
		if cachedAtVal, ok := metadata["cached_at"].(float64); ok {
			cachedAt = int64(cachedAtVal)
		}

		if cachedAt > 0 && time.Since(time.Unix(cachedAt, 0)) > c.ttl {
			// 过期，跳过
			continue
		}

		// 3. 获取响应内容（用于最终返回）
		respKey := RedisKeyResponse + queryHash
		response, err := c.client.Get(ctx, respKey).Result()
		if err != nil {
			continue
		}

		// 构建候选条目
		query := ""
		if queryVal, ok := metadata["query"].(string); ok {
			query = queryVal
		}

		accessCount := 0
		if count, ok := metadata["access_count"].(float64); ok {
			accessCount = int(count)
		}

		candidates = append(candidates, SemanticCandidate{
			Query:       query,
			Response:    response,
			Vector:      vector,
			CachedAt:    cachedAt,
			AccessCount: accessCount,
		})
	}

	return candidates, nil
}

// checkAndEvict 检查并清理缓存大小
func (c *RedisSemanticCache) checkAndEvict(ctx context.Context) error {
	stats := c.Stats()
	if stats.ValidEntries < c.maxCacheSize {
		return nil
	}

	// 从索引集合中获取所有 hash
	hashes, err := c.client.SMembers(ctx, RedisKeyIndexSet).Result()
	if err != nil {
		return err
	}

	if len(hashes) == 0 {
		return nil
	}

	// 收集所有条目的访问信息
	type entryInfo struct {
		hash        string
		accessedAt  int64
		accessCount int
	}
	var allEntries []entryInfo

	for _, queryHash := range hashes {
		// 获取元数据
		metaKey := RedisKeyMetadata + queryHash
		metaData, err := c.client.Get(ctx, metaKey).Bytes()
		if err != nil {
			continue
		}

		var metadata map[string]interface{}
		if err := json.Unmarshal(metaData, &metadata); err != nil {
			continue
		}

		accessedAt := int64(0)
		if accessedAtVal, ok := metadata["accessed_at"].(float64); ok {
			accessedAt = int64(accessedAtVal)
		}

		accessCount := 0
		if count, ok := metadata["access_count"].(float64); ok {
			accessCount = int(count)
		}

		allEntries = append(allEntries, entryInfo{
			hash:        queryHash,
			accessedAt:  accessedAt,
			accessCount: accessCount,
		})
	}

	if len(allEntries) == 0 {
		return nil
	}

	// 按访问时间和次数排序（淘汰访问次数少且久未访问的）
	sort.Slice(allEntries, func(i, j int) bool {
		// 优先淘汰访问次数少的
		if allEntries[i].accessCount != allEntries[j].accessCount {
			return allEntries[i].accessCount < allEntries[j].accessCount
		}
		// 访问次数相同，淘汰访问时间早的
		return allEntries[i].accessedAt < allEntries[j].accessedAt
	})

	// 删除最旧的条目
	deleteCount := len(allEntries) - c.maxCacheSize/2 // 删除到容量的一半
	if deleteCount > 0 {
		for i := 0; i < deleteCount && i < len(allEntries); i++ {
			queryHash := allEntries[i].hash

			// 使用 Pipeline 删除
			pipe := c.client.Pipeline()
			pipe.Del(ctx, RedisKeyResponse+queryHash)
			pipe.Del(ctx, RedisKeyVector+queryHash)
			pipe.Del(ctx, RedisKeyMetadata+queryHash)
			pipe.SRem(ctx, RedisKeyIndexSet, queryHash)
			pipe.Exec(ctx)
		}

		observability.L().InfoCtx(ctx, "RedisSemanticCache evicted entries", "count", deleteCount)
	}

	return nil
}

// hashQuery 使用 MD5 生成查询哈希
// MD5 相比简单哈希有更好的散列分布和更低的碰撞概率
func hashQuery(query string) string {
	hash := md5.Sum([]byte(query))
	return hex.EncodeToString(hash[:])
}

// extractHash 从键中提取哈希值
func extractHash(key string) string {
	// 键格式：semantic_cache:resp:{md5hash}
	// 尝试提取 MD5 哈希部分
	prefixes := []string{RedisKeyResponse, RedisKeyVector, RedisKeyMetadata}

	for _, prefix := range prefixes {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			return key[len(prefix):]
		}
	}
	return ""
}

// cosineSimilarity 计算余弦相似度
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct float64
	var normA, normB float64

	for i := 0; i < len(a); i++ {
		dotProduct += float64(a[i] * b[i])
		normA += float64(a[i] * a[i])
		normB += float64(b[i] * b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// CacheStats 缓存统计
type CacheStats struct {
	TotalEntries        int
	ValidEntries        int
	ExpiredEntries      int
	MaxEntries          int
	SimilarityThreshold float64
	TTLSeconds          int
	TotalAccessCount    int
	AverageAccessCount  int
}

// RedisClient 获取 Redis 客户端
func (c *RedisSemanticCache) RedisClient() *redis.Client {
	return c.client
}
