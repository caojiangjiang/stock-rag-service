package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"stock_rag/internal/agent"
	"stock_rag/internal/api"
	"stock_rag/internal/auth"
	"stock_rag/internal/cache"
	einoagent "stock_rag/internal/eino/agent"
	einomodel "stock_rag/internal/eino/model"
	ragretriever "stock_rag/internal/eino/retriever"
	einotools "stock_rag/internal/eino/tools"
	"stock_rag/internal/eino/trace"
	"stock_rag/internal/embedding"
	"stock_rag/internal/llm"
	"stock_rag/internal/memory"
	"stock_rag/internal/metrics"
	"stock_rag/internal/observability"
	"stock_rag/internal/pkg/limiter"
	"stock_rag/internal/pkgctx"
	"stock_rag/internal/repository"
	"stock_rag/internal/router"
	"stock_rag/internal/service"
	"stock_rag/internal/vectorstore"

	"github.com/cloudwego/eino/callbacks"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

type unifiedDataStore interface {
	repository.DocumentRepository
	vectorstore.VectorStore
}

func main() {
	ctx := context.Background()
	loadDotEnv()

	// 初始化 OpenTelemetry（连接 Jaeger）
	otelShutdown, err := observability.InitTracer("stock_rag")
	if err != nil {
		log.Printf("Warning: Failed to initialize OpenTelemetry: %v", err)
	} else {
		defer otelShutdown(ctx)
	}

	config, port := loadAppConfig()
	store := initUnifiedStore(ctx, config.Database.Postgres)
	embedder := initEmbedder(ctx, config.Embeddingder)
	initTracing()
	initGlobalLLM(ctx, config)
	defer llm.Close()

	// 初始化 Redis 客户端（共享给语义缓存、精确缓存、限流和健康检查）
	redisClient := initRedisClient(config.Database.Redis)

	querySvc := initQueryService(ctx, store, embedder, redisClient)
	conversationStore, pgConversationStore := initConversationStore(config.Database.Postgres)
	toolRegistry := initToolRegistry(querySvc)
	taskAgentService := initTaskAgentService(toolRegistry, conversationStore)
	authService, jwtSecret := initAuthService(pgConversationStore, redisClient)

	chatService := initChatService(querySvc, conversationStore, pgConversationStore, taskAgentService, toolRegistry, store, embedder, redisClient)
	mux := api.NewRouter(querySvc, taskAgentService, authService, jwtSecret, chatService, conversationStore, pgConversationStore.DB(), redisClient)

	// 添加限流中间件（如果 Redis 可用，使用分布式限流；否则使用内存限流）
	var rateLimiter limiter.RateLimiter
	if redisClient != nil {
		rateLimiter = limiter.NewRedisTokenBucket(redisClient, limiter.TokenBucketConfig{
			Capacity:   100,           // 桶容量：100 个令牌
			RefillRate: 10,            // 每秒补充 10 个令牌
		}, "httpratelimit:")
		log.Println("Rate limiter initialized (Redis distributed)")
	} else {
		rateLimiter = limiter.NewTokenBucket(limiter.TokenBucketConfig{
			Capacity:   100,
			RefillRate: 10,
		})
		log.Println("Rate limiter initialized (in-memory)")
	}

	// 限流中间件：按 IP 限流
	handler := limiter.RateLimitMiddleware(rateLimiter, limiter.DefaultKeyFunc)(mux)
	// 追踪中间件
	handler = observability.TracingMiddleware("stock_rag")(handler)
	// Prometheus 指标中间件（最外层，统计完整请求耗时）
	handler = metrics.HTTPMetricsMiddleware(handler)
	serveHTTP(port, handler)
}

func loadDotEnv() {
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: Failed to load .env file: %v", err)
	}
	initObservabilityLogging()
}

// initObservabilityLogging 默认把结构化 JSON 日志写入 logs/app.log，供 Promtail → Loki 与 Tempo trace 关联。
func initObservabilityLogging() {
	if os.Getenv("LOG_FILE") == "" {
		_ = os.Setenv("LOG_FILE", "logs/app.log")
	}
	observability.L().Info("structured logging ready", "log_file", os.Getenv("LOG_FILE"))
}

func initTracing() {
	traceHandler := trace.CreateAPMPlusCallback()
	callbacks.AppendGlobalHandlers(traceHandler)
	log.Println("APMPlus trace callback initialized successfully")
}

func loadAppConfig() (pkgctx.AppConfig, string) {
	configPath := "configs/config.yaml"
	config, err := pkgctx.LoadConfig(configPath)
	if err != nil {
		log.Printf("Warning: Failed to load config file: %v, using default config", err)
	}
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = fmt.Sprintf("%d", config.HTTP.Port)
	}
	return config, port
}

func initUnifiedStore(ctx context.Context, postgresConfig pkgctx.PostgresConfig) unifiedDataStore {
	store, err := vectorstore.NewPgVectorStore(
		ctx,
		postgresConfig.Host,
		postgresConfig.Port,
		postgresConfig.User,
		postgresConfig.Password,
		postgresConfig.Database,
		postgresConfig.SSLMode,
	)
	if err != nil {
		log.Fatalf("Failed to initialize unified PostgreSQL document/vector store in strict DB only mode: %v", err)
	}
	log.Println("Using unified PostgreSQL document/vector store (strict DB only mode)")
	return store
}

func initEmbedder(ctx context.Context, cfg pkgctx.EmbeddingderConfig) embedding.Embedder {
	arkEmbedder, err := embedding.NewArkEmbedder(ctx, embedding.DefaultArkEmbedderConfig(cfg))
	if err != nil {
		log.Printf("Warning: Failed to initialize Ark embedder: %v, using fallback", err)
		return embedding.NewSimpleEmbedder()
	}
	return arkEmbedder
}

func initGlobalLLM(ctx context.Context, config pkgctx.AppConfig) {
	chatModel, err := einomodel.NewChatModel(ctx, einomodel.DefaultChatConfig(config.Model))
	if err != nil {
		log.Fatalf("Failed to initialize chat model: %v", err)
	}
	llm.InitLLMClientWithMaxWait(
		chatModel,
		config.LLM.MaxQueueSize,
		config.LLM.MaxConcurrency,
		time.Duration(config.LLM.MaxWaitTimeMs)*time.Millisecond,
	)
}

func initQueryService(ctx context.Context, store unifiedDataStore, embedder embedding.Embedder, redisClient *redis.Client) *service.QueryService {
	allowLocalSampleRetrieval := strings.EqualFold(strings.TrimSpace(os.Getenv("ENABLE_LOCAL_ONLY_RETRIEVAL")), "true")
	ragHybridRetriever := ragretriever.NewHybridRetriever(ragretriever.HybridRetrieverConfig{
		Store:                    store,
		Embedder:                 embedder,
		VectorStore:              store,
		LoadLocalSampleDocuments: allowLocalSampleRetrieval,
	})

	deps := service.QueryServiceDependencies{
		Retriever:          ragHybridRetriever,
		DocumentRepository: store,
		VectorStore:        store,
		Embedder:           embedder,
		LLMClient:          llm.GetLLMClient(),
	}
	if redisClient != nil && embedder != nil {
		deps.RedisClient = redisClient
		deps.SemanticCacheConfig = cache.DefaultSemanticCacheConfig()
	}

	querySvc, err := service.NewQueryServiceWithDependencies(ctx, deps)
	if err != nil {
		log.Fatalf("init query service: %v", err)
	}
	if deps.RedisClient != nil {
		log.Println("Semantic cache enabled for query service")
	} else {
		log.Println("Warning: Semantic cache disabled for query service (Redis or embedder unavailable)")
	}
	if allowLocalSampleRetrieval {
		log.Println("local-only retrieval enabled for explicit evaluation/development requests")
	} else {
		log.Println("local-only retrieval disabled; production path uses unified persistent data only")
	}
	return querySvc
}

func initConversationStore(postgresConfig pkgctx.PostgresConfig) (repository.UnifiedConversationStore, *repository.PostgresConversationStore) {
	pgConversationStore, err := repository.NewPostgresConversationStore(
		postgresConfig.Host,
		postgresConfig.Port,
		postgresConfig.User,
		postgresConfig.Password,
		postgresConfig.Database,
		postgresConfig.SSLMode,
	)
	if err != nil {
		log.Printf("Warning: Failed to initialize PostgreSQL conversation store: %v, using memory storage as fallback", err)
		return repository.NewMemoryConversationStore(), nil
	}
	log.Println("Successfully initialized PostgreSQL conversation store")
	return pgConversationStore, pgConversationStore
}

func initToolRegistry(querySvc *service.QueryService) *einotools.ToolRegistry {
	toolRegistry := einotools.NewToolRegistry()
	if err := toolRegistry.RegisterStandardTools(querySvc); err != nil {
		log.Fatalf("Failed to register standard tools: %v", err)
	}
	return toolRegistry
}

func initTaskAgentService(toolRegistry *einotools.ToolRegistry, conversationStore repository.UnifiedConversationStore) *service.TaskAgentService {
	// 使用新的 Coordinator 体系
	// 1. 创建 ProfileRegistry 并注册默认 profiles
	profileRegistry := einoagent.NewProfileRegistry()

	// 2. 使用共享 ToolRegistry

	// 3. 创建 AgentBuilder
	agentBuilder := einoagent.NewAgentBuilder(toolRegistry)

	// 4. 创建 CoordinatorFactory
	coordinatorFactory := einoagent.NewCoordinatorFactory(profileRegistry, agentBuilder, toolRegistry)

	// 5. 创建 Coordinator（默认 supervisor，可通过 COORDINATOR_TYPE 切换）
	coordinatorType := resolveCoordinatorType()
	coordinator, err := coordinatorFactory.Create(coordinatorType)
	if err != nil {
		log.Fatalf("Failed to create coordinator %s: %v", coordinatorType, err)
	}
	log.Printf("Task agent coordinator: %s", coordinatorType)

	// 6. 设置子 Agent profiles
	coordinator.SetAgentProfiles([]*einoagent.AgentProfile{
		einoagent.EvidenceCollectorProfile,
		einoagent.MetricExtractorProfile,
		einoagent.AnalystWriterProfile,
	})

	// 7. 使用 CoordinatorSupervisorAdapter（含超时与协调级重试）
	supervisorAdapter := einoagent.NewCoordinatorSupervisorAdapter(coordinator)

	return service.NewTaskAgentService(supervisorAdapter, conversationStore)
}

func initAuthService(pgConversationStore *repository.PostgresConversationStore, redisClient *redis.Client) (auth.AuthService, string) {
	var userStore auth.UserStore
	var sessionStore auth.SessionStore
	if pgConversationStore != nil {
		pgUserStore := auth.NewPostgresUserStore(pgConversationStore.DB())
		if err := pgUserStore.InitTable(); err != nil {
			log.Printf("Warning: Failed to initialize PostgreSQL user store: %v, using memory storage as fallback", err)
			userStore = auth.NewMemoryUserStore()
		} else {
			log.Println("Successfully initialized PostgreSQL user store")
			userStore = pgUserStore
		}

		pgSessionStore := auth.NewPostgresSessionStore(pgConversationStore.DB())
		if err := pgSessionStore.InitTable(); err != nil {
			log.Printf("Warning: Failed to initialize PostgreSQL session store: %v, using memory storage as fallback", err)
			sessionStore = auth.NewMemorySessionStore()
		} else {
			log.Println("Successfully initialized PostgreSQL session store")
			sessionStore = pgSessionStore
		}
	} else {
		log.Println("Using memory storage for users and sessions")
		userStore = auth.NewMemoryUserStore()
		sessionStore = auth.NewMemorySessionStore()
	}

	jwtSecret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	if jwtSecret == "" {
		jwtSecret = "default-secret-key-change-in-production"
		log.Printf("Warning: Using default JWT secret. Set JWT_SECRET environment variable for production.")
	}
	cfg := auth.AuthServiceConfig{
		UserStore:     userStore,
		SessionStore:  sessionStore,
		JWTSecret:     jwtSecret,
		Blacklist:     auth.NewTokenBlacklist(redisClient),
		RefreshStore:  auth.NewRefreshStore(redisClient),
	}
	if redisClient != nil {
		log.Println("Auth token blacklist and refresh store using Redis")
	} else {
		log.Println("Warning: Redis unavailable, auth blacklist/refresh using in-memory store")
	}
	return auth.NewAuthServiceFromConfig(cfg), jwtSecret
}

func initChatService(querySvc *service.QueryService, conversationStore repository.UnifiedConversationStore, pgConversationStore *repository.PostgresConversationStore, taskAgentService *service.TaskAgentService, toolRegistry *einotools.ToolRegistry, store unifiedDataStore, embedder embedding.Embedder, redisClient *redis.Client) *agent.ChatService {
	routeEngine := router.NewRouteEngine(
		router.DefaultRouteConfig(),
		nil,
		router.NewHardRuleMatcher(),
		nil,
	)
	retriever := agent.NewQueryServiceRetriever(querySvc)
	chatExecutor := agent.NewChatExecutor(llm.GetLLMClient())
	ragExecutor := agent.NewRAGExecutor(llm.GetLLMClient(), retriever)
	analysisExecutor := agent.NewAnalysisExecutor(llm.GetLLMClient(), retriever)

	// ModeAgent 入口：默认 Supervisor；设置 AGENT_EXECUTOR=react 启用 ReAct 循环
	modeAgentExecutor := selectModeAgentExecutor(taskAgentService, toolRegistry)
	if modeAgentExecutor.Name() == "react_agent_executor" {
		log.Println("ModeAgent using ReAct executor (AGENT_EXECUTOR=react)")
	} else {
		log.Println("ModeAgent using Supervisor coordinator executor")
	}

	agentExecutor := agent.NewAgentExecutor(chatExecutor, ragExecutor, analysisExecutor, modeAgentExecutor)

	// 初始化精确缓存
	redisHost := strings.TrimSpace(os.Getenv("REDIS_HOST"))
	redisPort := strings.TrimSpace(os.Getenv("REDIS_PORT"))
	redisPassword := strings.TrimSpace(os.Getenv("REDIS_PASSWORD"))
	redisDB := 0

	var exactCache *cache.ExactCache
	// 如果没有传入 redisClient，创建一个新的
	if redisClient == nil && redisHost != "" {
		redisAddr := fmt.Sprintf("%s:%s", redisHost, redisPort)
		redisClient = redis.NewClient(&redis.Options{
			Addr:     redisAddr,
			Password: redisPassword,
			DB:       redisDB,
		})
	}

	if redisClient != nil {
		exactCache = cache.NewExactCache(redisClient, cache.DefaultExactCacheConfig())
		log.Println("Exact cache initialized for chat service")
	} else {
		log.Println("Warning: REDIS_HOST not set, exact cache disabled for chat service")
	}

	// 初始化记忆存储（短/中/长期）
	memDeps := memory.Dependencies{
		Redis:       redisClient,
		VectorStore: store,
		Embedder:    embedder,
	}
	if pgConversationStore != nil {
		memDeps.DB = pgConversationStore.DB()
	}
	mem := memory.New(memory.DefaultConfig(), memDeps)
	if pgConversationStore != nil {
		if err := mem.InitSchema(context.Background()); err != nil {
			log.Printf("Warning: Failed to initialize memory schema: %v", err)
		} else {
			log.Println("Memory stores (short/medium/long) initialized")
		}
	} else if redisClient != nil {
		log.Println("Working memory (short-term) initialized")
	} else {
		log.Println("Warning: memory stores disabled (no Redis/PostgreSQL)")
	}

	return agent.NewChatService(routeEngine, agentExecutor, conversationStore, exactCache, mem)
}

// selectModeAgentExecutor 按环境变量选择 ModeAgent 实现。
// AGENT_EXECUTOR=react 时使用 ReAct 思考-行动-观察循环；默认使用 Supervisor 多 Agent。
func resolveCoordinatorType() einoagent.CoordinatorType {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("COORDINATOR_TYPE"))) {
	case "plan":
		return einoagent.CoordinatorTypePlan
	case "pipeline":
		return einoagent.CoordinatorTypePipeline
	case "workflow":
		return einoagent.CoordinatorTypeWorkflow
	case "peer":
		return einoagent.CoordinatorTypePeer
	default:
		return einoagent.CoordinatorTypeSupervisor
	}
}

func selectModeAgentExecutor(taskAgentService *service.TaskAgentService, toolRegistry *einotools.ToolRegistry) agent.Executor {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("AGENT_EXECUTOR")), "react") {
		tools, err := agent.ToolsFromRegistry(toolRegistry)
		if err != nil {
			log.Fatalf("Failed to build ReAct tools from registry: %v", err)
		}
		return agent.NewReActAgentExecutor(llm.GetLLMClient(), tools, agent.ReActAgentExecutorConfig{
			MaxSteps:         10,
			MaxRetryAttempts: 1, // 工具层 ToolGuard 已含重试/熔断
		})
	}
	return agent.NewModeAgentExecutor(taskAgentService)
}

// initRedisClient 初始化 Redis 客户端（环境变量优先，其次 config.yaml）
func initRedisClient(redisCfg pkgctx.RedisConfig) *redis.Client {
	redisHost := strings.TrimSpace(os.Getenv("REDIS_HOST"))
	if redisHost == "" {
		redisHost = strings.TrimSpace(redisCfg.Host)
	}
	redisPort := strings.TrimSpace(os.Getenv("REDIS_PORT"))
	if redisPort == "" {
		redisPort = strings.TrimSpace(redisCfg.Port)
	}
	if redisPort == "" {
		redisPort = "6379"
	}
	redisPassword := strings.TrimSpace(os.Getenv("REDIS_PASSWORD"))
	if redisPassword == "" {
		redisPassword = redisCfg.Password
	}

	if redisHost == "" {
		log.Println("Warning: Redis host not configured, Redis client not initialized")
		return nil
	}

	redisAddr := fmt.Sprintf("%s:%s", redisHost, redisPort)
	redisClient := redis.NewClient(&redis.Options{
		Addr:         redisAddr,
		Password:     redisPassword,
		DB:           0,
		PoolSize:     50,              // 连接池大小
		MinIdleConns: 10,              // 最小空闲连接数
		PoolTimeout:  4 * time.Second, // 连接池获取连接超时
		ReadTimeout:  3 * time.Second, // 读取超时
		WriteTimeout: 3 * time.Second, // 写入超时
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Printf("Warning: Failed to connect to Redis: %v", err)
		return nil
	}

	log.Println("Redis client initialized successfully")
	return redisClient
}

func serveHTTP(port string, handler http.Handler) {
	addr := ":" + port
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      130 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("stock_rag listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}
	log.Println("server stopped")
}
