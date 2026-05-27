package metrics

import (
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	cacheHitsAtomic   atomic.Uint64
	cacheMissesAtomic atomic.Uint64
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
		[]string{"status", "mode"},
	)

	ChatRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "chat_request_duration_seconds",
			Help:    "Chat request latency in seconds",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120},
		},
		[]string{"mode"},
	)

	// LLM 队列（由 UpdateLLMQueueGauges 同步）
	LLMQueuePending = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "llm_queue_pending",
			Help: "Number of LLM requests waiting in queue",
		},
	)

	LLMQueueActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "llm_queue_active",
			Help: "Number of LLM requests currently processing",
		},
	)

	LLMQueueMaxSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "llm_queue_max_size",
			Help: "Maximum LLM queue capacity",
		},
	)

	LLMQueueAvgWaitMs = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "llm_queue_avg_wait_ms",
			Help: "Average wait time in LLM queue milliseconds",
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

	// 工具调用指标
	ToolCallsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tool_calls_total",
			Help: "Total number of tool invocations",
		},
		[]string{"tool", "status"},
	)

	ToolCallDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tool_call_duration_seconds",
			Help:    "Tool invocation latency in seconds",
			Buckets: []float64{0.05, 0.1, 0.5, 1, 2, 5, 10, 30, 60},
		},
		[]string{"tool"},
	)

	ToolCircuitOpenTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tool_circuit_open_total",
			Help: "Total number of tool calls rejected by circuit breaker",
		},
		[]string{"tool"},
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

	AgentCoordinatorTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agent_coordinator_total",
			Help: "Total coordinator executions",
		},
		[]string{"coordinator", "classifier", "status"},
	)

	AgentCoordinatorDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "agent_coordinator_duration_seconds",
			Help:    "Coordinator execution latency in seconds",
			Buckets: []float64{1, 2, 5, 10, 30, 60, 120, 300},
		},
		[]string{"coordinator"},
	)

	AgentSubtaskTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agent_subtask_total",
			Help: "Total sub-agent or subtask invocations within coordinators",
		},
		[]string{"coordinator", "subtask", "status"},
	)

	// Coordinator 步骤级指标
	AgentStepTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agent_coordinator_step_total",
			Help: "Total steps executed within coordinators",
		},
		[]string{"coordinator", "step", "status"},
	)

	AgentStepDurationVec = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "agent_coordinator_step_duration_seconds",
			Help:    "Coordinator step latency in seconds",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30},
		},
		[]string{"coordinator", "step"},
	)

	AgentComplexTaskTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agent_complex_task_total",
			Help: "Total complex agent task requests (e.g. analyze-stock)",
		},
		[]string{"endpoint", "status"},
	)

	AgentComplexTaskDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "agent_complex_task_duration_seconds",
			Help:    "Complex agent task latency in seconds",
			Buckets: []float64{1, 2, 5, 10, 30, 60, 120, 300},
		},
		[]string{"endpoint"},
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
	cacheHitsAtomic.Add(1)
	updateCacheHitRatio()
}

// RecordCacheMiss 记录缓存未命中
func RecordCacheMiss(cacheType string) {
	CacheMissesTotal.WithLabelValues(cacheType).Inc()
	cacheMissesAtomic.Add(1)
	updateCacheHitRatio()
}

func updateCacheHitRatio() {
	hits := cacheHitsAtomic.Load()
	misses := cacheMissesAtomic.Load()
	total := hits + misses
	if total == 0 {
		CacheHitRatio.Set(0)
		return
	}
	CacheHitRatio.Set(float64(hits) / float64(total))
}

// RecordHTTPDuration 记录 HTTP 请求延迟
func RecordHTTPDuration(method, path string, seconds float64) {
	HTTPRequestDuration.WithLabelValues(method, path).Observe(seconds)
}

// RecordChatRequest 记录聊天请求（按路由 mode 与状态）。
func RecordChatRequest(mode, status string, seconds float64) {
	if mode == "" {
		mode = "unknown"
	}
	ChatRequestsTotal.WithLabelValues(status, mode).Inc()
	ChatRequestDuration.WithLabelValues(mode).Observe(seconds)
}

// RecordToolCall 记录工具调用。
func RecordToolCall(tool, status string, seconds float64) {
	ToolCallsTotal.WithLabelValues(tool, status).Inc()
	ToolCallDuration.WithLabelValues(tool).Observe(seconds)
	if status == "circuit_open" {
		ToolCircuitOpenTotal.WithLabelValues(tool).Inc()
	}
}

// RecordRAGRetrieval 记录 RAG 检索
func RecordRAGRetrieval(status, stage string, seconds float64, resultCount int) {
	RAGRetrievalTotal.WithLabelValues(status).Inc()
	RAGRetrievalDuration.WithLabelValues(stage).Observe(seconds)
	RAGRetrievalResults.Observe(float64(resultCount))
}

// RecordAgentCoordinator 记录协调器整体执行。
func RecordAgentCoordinator(coordinator, classifier, status string, seconds float64) {
	AgentCoordinatorTotal.WithLabelValues(coordinator, classifier, status).Inc()
	AgentCoordinatorDuration.WithLabelValues(coordinator).Observe(seconds)
}

// RecordAgentSubtask 记录协调器内子任务/子 Agent 调用。
func RecordAgentSubtask(coordinator, subtask, status string, seconds float64) {
	AgentSubtaskTotal.WithLabelValues(coordinator, subtask, status).Inc()
	if seconds > 0 {
		AgentStepDuration.Observe(seconds)
	}
}

// RecordAgentStep 记录协调器内单个步骤的执行指标。
func RecordAgentStep(coordinator, stepName, status string, seconds float64) {
	AgentStepTotal.WithLabelValues(coordinator, stepName, status).Inc()
	if seconds > 0 {
		AgentStepDurationVec.WithLabelValues(coordinator, stepName).Observe(seconds)
	}
}

// RecordAgentComplexTask 记录复杂任务 API（如 analyze-stock）。
func RecordAgentComplexTask(endpoint, status string, seconds float64) {
	AgentComplexTaskTotal.WithLabelValues(endpoint, status).Inc()
	AgentComplexTaskDuration.WithLabelValues(endpoint).Observe(seconds)
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
