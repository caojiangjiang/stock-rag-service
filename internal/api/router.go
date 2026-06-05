package api

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"stock_rag/internal/agent"
	"stock_rag/internal/auth"
	einoagent "stock_rag/internal/eino/agent"
	"stock_rag/internal/metrics"
	personaservice "stock_rag/internal/persona/service"
	"stock_rag/internal/pkg/httpmiddleware"
	"stock_rag/internal/portfolio"
	"stock_rag/internal/repository"
	"stock_rag/internal/service"
	"stock_rag/internal/theme"
)

// Route 描述第一版计划暴露的接口。
type Route struct {
	Method string
	Path   string
	Usage  string
}

// NewRouter 注册当前已经接通的 HTTP 路由。
func NewRouter(querySvc QueryService, taskAgentService *service.TaskAgentService, authService auth.AuthService, jwtSecret string, chatService *agent.ChatService, conversationStore repository.UnifiedConversationStore, postgresDB Pinger, redisClient *redis.Client, coordinatorFactory *einoagent.CoordinatorFactory, portfolioSvc *portfolio.Service, themeSvc *theme.Service) *http.ServeMux {
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

	mux.HandleFunc("/stats", StatsHandler()) // 见 stats_handler.go
	longRunning := httpmiddleware.Timeout(120 * time.Second)
	requireAuth := func(h http.HandlerFunc) http.HandlerFunc {
		return ProtectHandler(authService, h)
	}
	requireAdmin := func(h http.HandlerFunc) http.HandlerFunc {
		return ProtectAdminHandler(authService, h)
	}

	mux.HandleFunc("/documents/import", requireAdmin(DocumentsImportHandler(querySvc)))
	mux.HandleFunc("/documents", DocumentsListHandler(querySvc))

	// 可选的 RAG API（用于内部测试和向后兼容）
	enableRAGAPI := strings.EqualFold(strings.TrimSpace(os.Getenv("ENABLE_RAG_API")), "true")
	if enableRAGAPI {
		mux.HandleFunc("/rag/query", longRunning(QueryHandler(querySvc)))
		mux.HandleFunc("/rag/query/stream", longRunning(QueryStreamHandler(querySvc)))
	} else {
		// 默认返回 404，提示使用 /api/chat
		mux.HandleFunc("/rag/query", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		mux.HandleFunc("/rag/query/stream", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
	}

	agentHandler := NewAgentHandler(taskAgentService, jwtSecret)
	mux.HandleFunc("/api/agent/execute", requireAuth(agentHandler.ExecuteTask))
	mux.HandleFunc("/api/agent/analyze-stock", requireAuth(agentHandler.AnalyzeStock))
	mux.HandleFunc("/api/agent/run", requireAuth(agentHandler.RunAgent))
	mux.HandleFunc("/api/agent/resume", requireAuth(agentHandler.ResumeAgent))
	mux.HandleFunc("/api/agent/session", requireAuth(agentHandler.GetSession))

	chatHandler := NewChatHandler(chatService)
	mux.HandleFunc("/api/chat", longRunning(requireAuth(chatHandler.Chat)))
	mux.HandleFunc("/api/chat/stream", longRunning(requireAuth(chatHandler.ChatStream)))

	convHandler := NewConversationHandler(conversationStore)
	mux.HandleFunc("/api/conversations", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			convHandler.UpdateConversation(w, r)
		} else {
			convHandler.ListConversations(w, r)
		}
	}))
	mux.HandleFunc("/api/conversations/get", requireAuth(convHandler.GetConversation))
	mux.HandleFunc("/api/conversations/messages", requireAuth(convHandler.GetConversationMessages))
	mux.HandleFunc("/api/conversations/create", requireAuth(convHandler.CreateConversation))
	mux.HandleFunc("/api/conversations/delete", requireAuth(convHandler.DeleteConversation))

	RegisterAuthRoutes(mux, authService, jwtSecret)

	// 个人持仓
	if portfolioSvc != nil {
		portfolioHandler := NewPortfolioHandler(portfolioSvc)
		mux.HandleFunc("/api/portfolio/positions", requireAuth(portfolioHandler.HandlePositions))
		mux.HandleFunc("/api/portfolio/summary", requireAuth(portfolioHandler.Summary))
		mux.HandleFunc("/api/portfolio/import/fund", requireAuth(portfolioHandler.ImportFund))
	}

	marketHandler := NewMarketHandler()
	mux.HandleFunc("/api/market/fund/nav", requireAuth(marketHandler.FundNAV))
	mux.HandleFunc("/api/market/stock/quote", requireAuth(marketHandler.StockQuote))

	// 主题快照
	if themeSvc != nil {
		themeHandler := NewThemeHandler(themeSvc)
		mux.HandleFunc("/api/themes/snapshot", requireAuth(themeHandler.Snapshot))
	}

	// Investment Persona 模块路由
	enablePersonaModule := strings.EqualFold(strings.TrimSpace(os.Getenv("ENABLE_PERSONA_MODULE")), "true")
	if enablePersonaModule {
		// 尝试加载配置，如果失败则回退到 mock
		var personaSvc personaservice.PersonaService
		configSvc, err := personaservice.NewConfigPersonaService("configs/personas.yaml", querySvc, coordinatorFactory)
		if err != nil {
			log.Printf("Warning: Failed to load persona config, using mock: %v", err)
			personaSvc = personaservice.NewMockPersonaService()
		} else {
			log.Println("Persona config loaded successfully")
			personaSvc = configSvc

			// 初始化每日选股服务（仅在配置加载成功时）
			enableDailyPicks := strings.EqualFold(strings.TrimSpace(os.Getenv("ENABLE_PERSONA_DAILY_PICKS")), "true")
			if enableDailyPicks {
				dailyPicksSvc, err := personaservice.NewDailyPicksService(
					"configs/personas.yaml",
					"configs/persona_daily_picks_universe.yaml",
					querySvc,
				)
				if err != nil {
					log.Printf("Warning: Failed to load daily picks service: %v", err)
				} else {
					log.Println("Daily picks service loaded successfully")
					dailyPicksHandler := NewDailyPicksHandler(dailyPicksSvc)
					mux.HandleFunc("/api/personas/daily-picks", dailyPicksHandler.GetDailyPicks)
					mux.HandleFunc("/api/personas/daily-picks/run", requireAdmin(dailyPicksHandler.RunManualTrigger))
				}
			}
		}

		personaHandler := NewPersonaHandler(personaSvc)
		mux.HandleFunc("/api/personas", personaHandler.ListPersonas)
		mux.HandleFunc("/api/personas/{id}", personaHandler.GetPersona)
		mux.HandleFunc("/api/personas/chat", requireAuth(personaHandler.Chat))

		// Roundtable 独立 feature flag
		enablePersonaRoundtable := strings.EqualFold(strings.TrimSpace(os.Getenv("ENABLE_PERSONA_ROUNDTABLE")), "true")
		if enablePersonaRoundtable {
			mux.HandleFunc("/api/personas/roundtable", requireAuth(personaHandler.Roundtable))
		} else {
			mux.HandleFunc("/api/personas/roundtable", func(w http.ResponseWriter, r *http.Request) {
				http.NotFound(w, r)
			})
		}
	} else {
		// feature flag 关闭时返回 404
		mux.HandleFunc("/api/personas", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		mux.HandleFunc("/api/personas/{id}", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		mux.HandleFunc("/api/personas/chat", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		mux.HandleFunc("/api/personas/roundtable", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
	}

	// 监控面板路由 - 反向代理到各个监控系统（移除认证保护，因为新标签页无法传递 Authorization header）
	mux.Handle("/monitor/", createMonitorProxy("http://localhost:3000", "/monitor"))
	mux.Handle("/prometheus/", createMonitorProxy("http://localhost:9091", "/prometheus"))
	mux.Handle("/jaeger/", createMonitorProxy("http://localhost:16686", "/jaeger"))
	mux.Handle("/tempo/", createMonitorProxy("http://localhost:3200", "/tempo"))
	mux.Handle("/loki/", createMonitorProxy("http://localhost:3100", "/loki"))

	// 静态文件服务，用于前端界面
	mux.Handle("/", http.FileServer(http.Dir("web")))

	return mux
}

// createMonitorProxy 创建监控系统的反向代理
func createMonitorProxy(target string, prefix string) http.HandlerFunc {
	targetURL, _ := url.Parse(target)
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	return func(w http.ResponseWriter, r *http.Request) {
		// 修改请求路径，移除前缀
		r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
		r.Host = targetURL.Host

		// 修改响应头，处理重定向
		proxy.ModifyResponse = func(resp *http.Response) error {
			if loc := resp.Header.Get("Location"); loc != "" {
				// 将监控系统的重定向修改为通过前缀
				resp.Header.Set("Location", prefix+loc)
			}
			return nil
		}

		proxy.ServeHTTP(w, r)
	}
}

// monitorProxyHandler 创建监控面板的反向代理（保留兼容）
func monitorProxyHandler() http.HandlerFunc {
	return createMonitorProxy("http://localhost:3000", "/monitor")
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
		{Method: "POST", Path: "/api/chat/stream", Usage: "统一聊天流式接口（SSE）"},
		{Method: "POST", Path: "/agent/execute", Usage: "执行 Agent 任务"},
		{Method: "GET", Path: "/agent/analyze-stock", Usage: "分析股票"},
	}
}
