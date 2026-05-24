package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"

	"stock_rag/internal/agent"
	"stock_rag/internal/auth"
	"stock_rag/internal/llm"
	"stock_rag/internal/metrics"
	"stock_rag/internal/pkg/httpmiddleware"
	"stock_rag/internal/repository"
	"stock_rag/internal/service"
)

// Route 描述第一版计划暴露的接口。
type Route struct {
	Method string
	Path   string
	Usage  string
}

// NewRouter 注册当前已经接通的 HTTP 路由。
func NewRouter(querySvc QueryService, taskAgentService *service.TaskAgentService, authService auth.AuthService, jwtSecret string, chatService *agent.ChatService, conversationStore repository.UnifiedConversationStore, postgresDB Pinger, redisClient *redis.Client) *http.ServeMux {
	mux := http.NewServeMux()

	// 健康检查端点
	healthDeps := HealthDependencies{
		PostgresDB:  postgresDB,
		RedisClient: redisClient,
	}
	mux.HandleFunc("/health/liveness", LivenessHandler())
	mux.HandleFunc("/health/readiness", ReadinessHandler(healthDeps))
	mux.HandleFunc("/health", HealthHandler()) // 保留兼容旧版本

	// Prometheus metrics 端点
	mux.Handle("/metrics", metrics.Handler())

	mux.HandleFunc("/stats", StatsHandler())
	mux.HandleFunc("/documents/import", DocumentsImportHandler(querySvc))
	mux.HandleFunc("/documents", DocumentsListHandler(querySvc))
	longRunning := httpmiddleware.Timeout(120 * time.Second)
	mux.HandleFunc("/rag/query", longRunning(QueryHandler(querySvc)))
	mux.HandleFunc("/rag/query/stream", longRunning(QueryStreamHandler(querySvc)))

	agentHandler := NewAgentHandler(taskAgentService, jwtSecret)
	mux.HandleFunc("/api/agent/execute", agentHandler.ExecuteTask)
	mux.HandleFunc("/api/agent/analyze-stock", agentHandler.AnalyzeStock)
	mux.HandleFunc("/api/agent/run", agentHandler.RunAgent)
	mux.HandleFunc("/api/agent/session", agentHandler.GetSession)

	// 统一聊天接口
	chatHandler := NewChatHandler(chatService)
	mux.HandleFunc("/api/chat", longRunning(chatHandler.Chat))

	// 对话管理接口
	convHandler := NewConversationHandler(conversationStore)
	mux.HandleFunc("/api/conversations", convHandler.ListConversations)
	mux.HandleFunc("/api/conversations/get", convHandler.GetConversation)
	mux.HandleFunc("/api/conversations/messages", convHandler.GetConversationMessages)
	mux.HandleFunc("/api/conversations/create", convHandler.CreateConversation)
	mux.HandleFunc("/api/conversations/delete", convHandler.DeleteConversation)

	RegisterAuthRoutes(mux, authService, jwtSecret)

	// 静态文件服务，用于前端界面
	mux.Handle("/", http.FileServer(http.Dir("web")))

	return mux
}

// SystemStats 系统性能面板

type SystemStats struct {
	Query  QueryStats  `json:"query"`
	Stream StreamStats `json:"stream"`
	Agent  AgentStats  `json:"agent"`
	Cache  CacheStats  `json:"cache"`
	Queue  QueueStats  `json:"queue"`
	Error  ErrorStats  `json:"error"`
}

// QueryStats 查询指标
type QueryStats struct {
	Total      int64   `json:"total"`
	Success    int64   `json:"success"`
	Failure    int64   `json:"failure"`
	AvgLatency float64 `json:"avg_latency_ms"`
	P95Latency float64 `json:"p95_latency_ms"`
}

// StreamStats 流式指标
type StreamStats struct {
	Total         int64   `json:"total"`
	Success       int64   `json:"success"`
	Failure       int64   `json:"failure"`
	AvgFirstToken float64 `json:"avg_first_token_ms"`
	P95FirstToken float64 `json:"p95_first_token_ms"`
}

// AgentStats Agent指标
type AgentStats struct {
	Total            int64         `json:"total"`
	AvgSteps         float64       `json:"avg_steps"`
	StepDistribution map[int]int64 `json:"step_distribution"`
	AvgStepLatency   float64       `json:"avg_step_latency_ms"`
}

// CacheStats 缓存指标
type CacheStats struct {
	HitRate float64 `json:"hit_rate"`
	Total   int64   `json:"total"`
	Hits    int64   `json:"hits"`
	Misses  int64   `json:"misses"`
}

// QueueStats 队列指标
type QueueStats struct {
	Pending    int     `json:"pending"`
	Processing int     `json:"processing"`
	MaxQueue   int     `json:"max_queue"`
	AvgWait    float64 `json:"avg_wait_ms"`
}

// ErrorStats 错误指标
type ErrorStats struct {
	Total          int64 `json:"total"`
	Timeout        int64 `json:"timeout"`
	LLMError       int64 `json:"llm_error"`
	RetrievalError int64 `json:"retrieval_error"`
	AgentError     int64 `json:"agent_error"`
}

// StatsHandler 返回系统性能面板
func StatsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		summary := metrics.GatherSummary()
		stats := SystemStats{
			Cache: CacheStats{
				HitRate: summary.Cache.HitRate,
				Total:   int64(summary.Cache.Hits + summary.Cache.Misses),
				Hits:    int64(summary.Cache.Hits),
				Misses:  int64(summary.Cache.Misses),
			},
			Error: ErrorStats{
				Total:          int64(summary.Chat.Error + summary.LLM.Error + summary.RAG.Error),
				LLMError:       int64(summary.LLM.Error),
				RetrievalError: int64(summary.RAG.Error),
				AgentError:     int64(summary.Chat.Error),
			},
		}

		// 获取LLM队列信息
		client := llm.GetLLMClient()
		if client != nil {
			queueStats := client.GetStats()
			stats.Queue = QueueStats{
				Pending:    queueStats["pending"].(int),
				Processing: queueStats["processing"].(int),
				MaxQueue:   queueStats["max_queue"].(int),
				AvgWait:    queueStats["avg_wait"].(float64),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	}
}

// DefaultRoutes 返回第一版推荐接口清单。
func DefaultRoutes() []Route {
	return []Route{
		{Method: "GET", Path: "/health", Usage: "健康检查"},
		{Method: "POST", Path: "/stocks/search", Usage: "股票搜索"},
		{Method: "POST", Path: "/documents/import", Usage: "文档导入"},
		{Method: "GET", Path: "/documents", Usage: "文档列表"},
		{Method: "POST", Path: "/rag/query", Usage: "普通问答"},
		{Method: "POST", Path: "/rag/query/stream", Usage: "流式问答"},
		{Method: "POST", Path: "/api/chat", Usage: "统一聊天接口（支持自动路由）"},
		{Method: "POST", Path: "/agent/execute", Usage: "执行 Agent 任务"},
		{Method: "GET", Path: "/agent/analyze-stock", Usage: "分析股票"},
	}
}
