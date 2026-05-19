package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"stock_rag/internal/agent"
	"stock_rag/internal/api"
	"stock_rag/internal/auth"
	einoagent "stock_rag/internal/eino/agent"
	einomodel "stock_rag/internal/eino/model"
	ragretriever "stock_rag/internal/eino/retriever"
	einotools "stock_rag/internal/eino/tools"
	"stock_rag/internal/eino/trace"
	"stock_rag/internal/embedding"
	"stock_rag/internal/llm"
	"stock_rag/internal/observability"
	"stock_rag/internal/pkgctx"
	"stock_rag/internal/repository"
	"stock_rag/internal/router"
	"stock_rag/internal/service"
	"stock_rag/internal/vectorstore"

	"github.com/cloudwego/eino/callbacks"
	"github.com/joho/godotenv"
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
	byteplusLogger := initTracing()
	defer byteplusLogger.Close()
	initGlobalLLM(ctx, config)
	defer llm.Close()
	querySvc := initQueryService(ctx, store, embedder)
	conversationStore, pgConversationStore := initConversationStore(config.Database.Postgres)
	taskAgentService := initTaskAgentService(conversationStore, querySvc)
	authService, jwtSecret := initAuthService(pgConversationStore)
	chatService := initChatService(querySvc, conversationStore, taskAgentService)

	mux := api.NewRouter(querySvc, taskAgentService, authService, jwtSecret, chatService, conversationStore, byteplusLogger)
	serveHTTP(port, mux)
}

func loadDotEnv() {
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: Failed to load .env file: %v", err)
	}
}

func initTracing() *trace.BytePlusLogger {
	byteplusLogger := trace.GetBytePlusLogger()
	trace.SetLogger(byteplusLogger)
	traceHandler := trace.CreateAPMPlusCallback(byteplusLogger)
	callbacks.AppendGlobalHandlers(traceHandler)
	log.Println("APMPlus trace callback initialized successfully")
	return byteplusLogger
}

func loadAppConfig() (pkgctx.AppConfig, string) {
	configPath := "configs/config.local.yaml"
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

func initQueryService(ctx context.Context, store unifiedDataStore, embedder embedding.Embedder) *service.QueryService {
	allowLocalSampleRetrieval := strings.EqualFold(strings.TrimSpace(os.Getenv("ENABLE_LOCAL_ONLY_RETRIEVAL")), "true")
	ragHybridRetriever := ragretriever.NewHybridRetriever(ragretriever.HybridRetrieverConfig{
		Store:                    store,
		Embedder:                 embedder,
		VectorStore:              store,
		LoadLocalSampleDocuments: allowLocalSampleRetrieval,
	})
	querySvc, err := service.NewQueryServiceWithDependencies(ctx, service.QueryServiceDependencies{
		Retriever:          ragHybridRetriever,
		DocumentRepository: store,
		VectorStore:        store,
		Embedder:           embedder,
		LLMClient:          llm.GetLLMClient(),
	})
	if err != nil {
		log.Fatalf("init query service: %v", err)
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

func initTaskAgentService(conversationStore repository.UnifiedConversationStore, querySvc *service.QueryService) *service.TaskAgentService {
	// 使用新的 Coordinator 体系
	// 1. 创建 ProfileRegistry 并注册默认 profiles
	profileRegistry := einoagent.NewProfileRegistry()

	// 2. 创建 ToolRegistry 并注册工具
	toolRegistry := einotools.NewToolRegistry()

	// 注册标准工具
	toolRegistry.RegisterStandardTools(querySvc)

	// 3. 创建 AgentBuilder
	agentBuilder := einoagent.NewAgentBuilder(toolRegistry)

	// 4. 创建 CoordinatorFactory
	coordinatorFactory := einoagent.NewCoordinatorFactory(profileRegistry, agentBuilder, toolRegistry)

	// 5. 创建 SupervisorCoordinator（使用 Coordinator 体系）
	coordinator, err := coordinatorFactory.Create(einoagent.CoordinatorTypeSupervisor)
	if err != nil {
		log.Fatalf("Failed to create SupervisorCoordinator: %v", err)
	}

	// 6. 设置子 Agent profiles
	coordinator.SetAgentProfiles([]*einoagent.AgentProfile{
		einoagent.EvidenceCollectorProfile,
		einoagent.MetricExtractorProfile,
		einoagent.AnalystWriterProfile,
	})

	// 7. 使用新的 CoordinatorSupervisorAdapter
	supervisorAdapter := einoagent.NewCoordinatorSupervisorAdapter(coordinator)

	return service.NewTaskAgentService(supervisorAdapter, conversationStore)
}

func initAuthService(pgConversationStore *repository.PostgresConversationStore) (auth.AuthService, string) {
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
	return auth.NewAuthServiceImpl(userStore, sessionStore, jwtSecret), jwtSecret
}

func initChatService(querySvc *service.QueryService, conversationStore repository.UnifiedConversationStore, taskAgentService *service.TaskAgentService) *agent.ChatService {
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

	// ModeAgent 的唯一入口：ModeAgentExecutor
	// 顶层 executor 只知道注入了一个 executor，不知道内部实现细节
	modeAgentExecutor := agent.NewModeAgentExecutor(taskAgentService)

	agentExecutor := agent.NewAgentExecutor(chatExecutor, ragExecutor, analysisExecutor, modeAgentExecutor)
	return agent.NewChatService(routeEngine, agentExecutor, conversationStore)
}

func serveHTTP(port string, mux *http.ServeMux) {
	addr := ":" + port
	log.Printf("stock_rag listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
