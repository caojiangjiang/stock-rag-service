package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTP 指标
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	HTTPRequestsInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "Number of HTTP requests currently being processed",
		},
	)

	// 业务指标
	ChatRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "chat_requests_total",
			Help: "Total number of chat requests",
		},
		[]string{"status"},
	)

	ChatRequestDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "chat_request_duration_seconds",
			Help:    "Chat request latency in seconds",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30},
		},
	)

	// RAG 检索指标
	RAGRetrievalTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rag_retrieval_total",
			Help: "Total number of RAG retrieval requests",
		},
		[]string{"status"},
	)

	RAGRetrievalDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "rag_retrieval_duration_seconds",
			Help:    "RAG retrieval latency in seconds",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5},
		},
		[]string{"stage"},
	)

	RAGRetrievalResults = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "rag_retrieval_results",
			Help:    "Number of results returned from RAG retrieval",
			Buckets: []float64{1, 5, 10, 20, 50, 100},
		},
	)

	// 缓存指标
	CacheHitsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cache_hits_total",
			Help: "Total number of cache hits",
		},
		[]string{"cache_type"},
	)

	CacheMissesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cache_misses_total",
			Help: "Total number of cache misses",
		},
		[]string{"cache_type"},
	)

	CacheHitRatio = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "cache_hit_ratio",
			Help: "Cache hit ratio",
		},
	)

	// LLM 调用指标
	LLMRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "llm_requests_total",
			Help: "Total number of LLM requests",
		},
		[]string{"status"},
	)

	LLMRequestDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "llm_request_duration_seconds",
			Help:    "LLM request latency in seconds",
			Buckets: []float64{0.5, 1, 2, 5, 10, 30, 60},
		},
	)

	LLMTokensTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "llm_tokens_total",
			Help: "Total number of LLM tokens",
		},
		[]string{"type"}, // prompt or completion
	)

	// Agent 指标
	AgentStepsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "agent_steps_total",
			Help: "Total number of agent steps executed",
		},
	)

	AgentStepDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "agent_step_duration_seconds",
			Help:    "Agent step latency in seconds",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
		},
	)

	// 数据库连接池指标
	DBPoolConnections = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "db_pool_connections",
			Help: "Number of database connections in the pool",
		},
		[]string{"state"}, // idle or active
	)

	// Redis 连接池指标
	RedisPoolConnections = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "redis_pool_connections",
			Help: "Number of Redis connections in the pool",
		},
		[]string{"state"}, // idle or active
	)

	// 错误指标
	ErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "errors_total",
			Help: "Total number of errors",
		},
		[]string{"type"},
	)
)

// RecordCacheHit 记录缓存命中
func RecordCacheHit(cacheType string) {
	CacheHitsTotal.WithLabelValues(cacheType).Inc()
}

// RecordCacheMiss 记录缓存未命中
func RecordCacheMiss(cacheType string) {
	CacheMissesTotal.WithLabelValues(cacheType).Inc()
}

// RecordHTTPDuration 记录 HTTP 请求延迟
func RecordHTTPDuration(method, path string, seconds float64) {
	HTTPRequestDuration.WithLabelValues(method, path).Observe(seconds)
}

// RecordChatRequest 记录聊天请求
func RecordChatRequest(status string, seconds float64) {
	ChatRequestsTotal.WithLabelValues(status).Inc()
	ChatRequestDuration.Observe(seconds)
}

// RecordRAGRetrieval 记录 RAG 检索
func RecordRAGRetrieval(stage string, seconds float64, resultCount int) {
	RAGRetrievalDuration.WithLabelValues(stage).Observe(seconds)
	RAGRetrievalResults.Observe(float64(resultCount))
}

// RecordLLMRequest 记录 LLM 请求
func RecordLLMRequest(status string, seconds float64, promptTokens, completionTokens int64) {
	LLMRequestsTotal.WithLabelValues(status).Inc()
	LLMRequestDuration.Observe(seconds)
	if promptTokens > 0 {
		LLMTokensTotal.WithLabelValues("prompt").Add(float64(promptTokens))
	}
	if completionTokens > 0 {
		LLMTokensTotal.WithLabelValues("completion").Add(float64(completionTokens))
	}
}
