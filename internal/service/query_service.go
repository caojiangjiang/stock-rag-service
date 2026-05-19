package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/compose"

	"stock_rag/internal/concurrency"
	ragchain "stock_rag/internal/eino/chain"
	ragretriever "stock_rag/internal/eino/retriever"
	"stock_rag/internal/embedding"
	appmodel "stock_rag/internal/model"
	"stock_rag/internal/repository"
	"stock_rag/internal/utils"
	"stock_rag/internal/vectorstore"
)

// QueryServiceDependencies 描述可注入的底层依赖。
type QueryServiceDependencies struct {
	LLMClient          *concurrency.LLMClient
	Retriever          ragretriever.Retriever
	DocumentRepository repository.DocumentRepository
	Embedder           embedding.Embedder
	VectorStore        vectorstore.VectorStore
}

// QueryService 是 API 层和 Eino 层之间的业务编排层。
type QueryService struct {
	Name          string
	runner        compose.Runnable[appmodel.RAGQueryRequest, appmodel.RAGQueryResponse]
	llmClient     *concurrency.LLMClient
	retriever     ragretriever.Retriever
	repo          repository.DocumentRepository
	embedder      embedding.Embedder
	vectors       vectorstore.VectorStore
	companyToCode map[string]string
	codeToCompany map[string]string
}

// NewQueryService 创建第一版查询服务。
func NewQueryService(ctx context.Context) (*QueryService, error) {
	return NewQueryServiceWithDependencies(ctx, QueryServiceDependencies{})
}

// NewQueryServiceWithDependencies 允许注入 repository / retriever / embedder / vector store。
func NewQueryServiceWithDependencies(ctx context.Context, deps QueryServiceDependencies) (*QueryService, error) {
	// 必须使用注入的 LLMClient，不允许在 service 内部创建新的 client
	// 这是为了确保全局统一的配额管理
	if deps.LLMClient == nil {
		return nil, fmt.Errorf("LLMClient must be provided, please use llm.GetLLMClient() to get the global instance")
	}
	llmClient := deps.LLMClient

	repo := deps.DocumentRepository
	if repo == nil {
		repo = repository.NewMemoryDocumentRepository()
	}

	retriever := deps.Retriever
	if retriever == nil {
		retriever = ragretriever.NewHybridRetriever(ragretriever.HybridRetrieverConfig{
			Store:       repo,
			Embedder:    deps.Embedder,
			VectorStore: deps.VectorStore,
		})
	}

	runner, err := ragchain.NewSkeletonRunnerWithLLMClient(ctx, llmClient, retriever)
	if err != nil {
		return nil, fmt.Errorf("failed to create skeleton runner: %w", err)
	}

	qs := &QueryService{
		Name:          "query_service",
		runner:        runner,
		llmClient:     llmClient,
		retriever:     retriever,
		repo:          repo,
		embedder:      deps.Embedder,
		vectors:       deps.VectorStore,
		companyToCode: make(map[string]string),
		codeToCompany: make(map[string]string),
	}

	// 加载股票代码映射
	if err := qs.loadStockCodeMappings(ctx); err != nil {
		utils.Warning("Failed to load stock code mappings", utils.LogFields{
			Message: err.Error(),
		})
	}

	// 启动定时刷新股票代码映射的协程
	go qs.startStockCodeMappingsRefresh()

	return qs, nil
}

// startStockCodeMappingsRefresh 启动定时刷新股票代码映射的协程
func (s *QueryService) startStockCodeMappingsRefresh() {
	// 每5分钟刷新一次
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		<-ticker.C
		ctx := context.Background()
		if err := s.loadStockCodeMappings(ctx); err != nil {
			utils.Warning("Failed to refresh stock code mappings", utils.LogFields{
				Message: err.Error(),
			})
		} else {
			utils.Info("Stock code mappings refreshed", utils.LogFields{
				Message: fmt.Sprintf("for %d companies", len(s.codeToCompany)),
			})
		}
	}
}

// ListDocuments 返回当前内存仓库中的全部文档。
func (s *QueryService) ListDocuments(ctx context.Context) (appmodel.DocumentsListResponse, error) {
	if s == nil || s.repo == nil {
		return appmodel.DocumentsListResponse{}, fmt.Errorf("document repository unavailable")
	}

	docs, err := s.repo.ListDocuments(ctx)
	if err != nil {
		return appmodel.DocumentsListResponse{}, err
	}

	return appmodel.DocumentsListResponse{
		Documents:  docs,
		TotalCount: len(docs),
	}, nil
}

// ImportDocuments 将同一批文档写入统一文档仓库和向量索引。
func (s *QueryService) ImportDocuments(ctx context.Context, req appmodel.DocumentsImportRequest) (appmodel.DocumentsImportResponse, error) {
	if s == nil || s.vectors == nil {
		return appmodel.DocumentsImportResponse{}, fmt.Errorf("vector store unavailable")
	}
	if s.embedder == nil {
		return appmodel.DocumentsImportResponse{}, fmt.Errorf("embedder unavailable")
	}
	if len(req.Documents) == 0 {
		return appmodel.DocumentsImportResponse{ImportedCount: 0, TotalCount: 0}, nil
	}

	texts := make([]string, 0, len(req.Documents))
	for _, doc := range req.Documents {
		texts = append(texts, buildIndexableText(doc))
	}

	vectors, err := s.embedder.EmbedDocuments(ctx, texts)
	if err != nil {
		return appmodel.DocumentsImportResponse{}, fmt.Errorf("embed documents: %w", err)
	}
	if len(vectors) != len(req.Documents) {
		return appmodel.DocumentsImportResponse{}, fmt.Errorf("embedder returned %d vectors for %d documents", len(vectors), len(req.Documents))
	}

	records := make([]vectorstore.Record, 0, len(req.Documents))
	for i, doc := range req.Documents {
		records = append(records, vectorstore.Record{
			ID:      buildDocumentRecordID(doc, i),
			Content: doc.Content,
			Citation: appmodel.Citation{
				Title:        doc.Title,
				DocType:      doc.DocType,
				SourceURL:    doc.SourceURL,
				Published:    doc.Published,
				SectionTitle: doc.Title,
			},
			Vector:   vectors[i],
			Metadata: documentMetadata(doc),
		})
	}

	if err := s.vectors.Upsert(ctx, records); err != nil {
		return appmodel.DocumentsImportResponse{}, fmt.Errorf("upsert records: %w", err)
	}
	if err := s.loadStockCodeMappings(ctx); err != nil {
		utils.Warning("Failed to refresh stock code mappings after import", utils.LogFields{Message: err.Error()})
	}

	return appmodel.DocumentsImportResponse{
		ImportedCount: len(records),
		TotalCount:    len(req.Documents),
	}, nil
}

func buildIndexableText(doc appmodel.Document) string {
	parts := make([]string, 0, 4)
	for _, part := range []string{doc.Title, doc.CompanyName, doc.Content, strings.Join(doc.Keywords, " ")} {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "\n")
}

func buildDocumentRecordID(doc appmodel.Document, index int) string {
	title := strings.TrimSpace(doc.Title)
	if title == "" {
		title = fmt.Sprintf("doc-%d", index)
	}
	return fmt.Sprintf("%s|%s|%s", strings.TrimSpace(doc.StockCode), strings.TrimSpace(doc.DocType), title)
}

func documentMetadata(doc appmodel.Document) map[string]string {
	keywords := append([]string(nil), doc.Keywords...)
	sort.Strings(keywords)
	return map[string]string{
		"stock_code":    strings.TrimSpace(doc.StockCode),
		"company_name":  strings.TrimSpace(doc.CompanyName),
		"doc_type":      strings.TrimSpace(doc.DocType),
		"title":         strings.TrimSpace(doc.Title),
		"source_url":    strings.TrimSpace(doc.SourceURL),
		"published_at":  strings.TrimSpace(doc.Published),
		"section_title": strings.TrimSpace(doc.Title),
		"source":        "documents/import",
		"content":       doc.Content,
		"raw_content":   doc.Content,
		"keywords":      strings.Join(keywords, ","),
	}
}

// extractStockCode 从问题中提取股票代码或公司名称
func (s *QueryService) extractStockCode(question string) string {
	// 转换为小写便于匹配
	lowerQuestion := strings.ToLower(question)

	// 检查公司名称
	for company, code := range s.companyToCode {
		if strings.Contains(lowerQuestion, strings.ToLower(company)) {
			return code
		}
	}

	// 检查股票代码
	for code := range s.codeToCompany {
		if strings.Contains(question, code) {
			return code
		}
	}

	return ""
}

// Query 执行当前阶段的最小问答链路。
func (s *QueryService) Query(ctx context.Context, req appmodel.RAGQueryRequest) (appmodel.RAGQueryResponse, error) {
	// 自动提取股票代码
	if req.StockCode == "" {
		req.StockCode = s.extractStockCode(req.Question)
		if req.StockCode != "" {
			utils.Info("自动提取股票代码", utils.LogFields{
				StockCode: req.StockCode,
			})
		}
	}

	utils.Info("开始执行RAG查询", utils.LogFields{
		StockCode: req.StockCode,
		TopK:      req.TopK,
		Message:   req.Question,
	})
	start := time.Now()

	resp, err := s.runner.Invoke(ctx, req)
	elapsed := time.Since(start)

	if err != nil {
		utils.Error("RAG查询失败", utils.LogFields{
			StockCode: req.StockCode,
			TopK:      req.TopK,
			Elapsed:   elapsed,
			Message:   err.Error(),
		})
		return resp, err
	}

	utils.Info("RAG查询完成", utils.LogFields{
		StockCode:      req.StockCode,
		TopK:           req.TopK,
		Elapsed:        elapsed,
		RetrievedCount: len(resp.Citations),
		Message:        fmt.Sprintf("生成答案长度: %d", len(resp.Answer)),
	})
	return resp, nil
}

// QueryStream 执行流式问答，并通过回调返回增量文本。
func (s *QueryService) QueryStream(ctx context.Context, req appmodel.RAGQueryRequest, onChunk func(string) error) (appmodel.RAGQueryResponse, error) {
	// 自动提取股票代码
	if req.StockCode == "" {
		req.StockCode = s.extractStockCode(req.Question)
		if req.StockCode != "" {
			utils.Info("自动提取股票代码", utils.LogFields{
				StockCode: req.StockCode,
			})
		}
	}

	utils.Info("开始执行流式RAG查询", utils.LogFields{
		StockCode: req.StockCode,
		TopK:      req.TopK,
		Message:   req.Question,
	})
	start := time.Now()

	utils.Info("开始准备查询", utils.LogFields{
		StockCode: req.StockCode,
		TopK:      req.TopK,
	})
	prepareStart := time.Now()
	prepared, err := ragchain.PrepareQuery(ctx, req, s.retriever)
	prepareElapsed := time.Since(prepareStart)
	if err != nil {
		utils.Error("准备查询失败", utils.LogFields{
			StockCode: req.StockCode,
			TopK:      req.TopK,
			Elapsed:   prepareElapsed,
			Message:   err.Error(),
		})
		return appmodel.RAGQueryResponse{}, err
	}
	utils.Info("查询准备完成", utils.LogFields{
		StockCode:      req.StockCode,
		TopK:           req.TopK,
		Elapsed:        prepareElapsed,
		RetrievedCount: prepared.RetrievedCount,
		RequestID:      prepared.RequestID,
	})

	// 创建流式请求
	utils.Info("开始调用大模型", utils.LogFields{
		StockCode: req.StockCode,
		TopK:      req.TopK,
		RequestID: prepared.RequestID,
	})
	llmStart := time.Now()
	llmReq := &concurrency.LLMRequest{
		RequestID: fmt.Sprintf("stream-%d", time.Now().UnixNano()),
		Question:  prepared.Request.Question,
		Messages:  prepared.Messages,
		TaskType:  "stream",
		Priority:  0,
		Timeout:   2 * time.Minute,
		Stream:    true,
		OnChunk:   onChunk,
		Metadata: map[string]string{
			"stock_code": prepared.Request.StockCode,
			"question":   prepared.Request.Question,
		},
	}

	utils.Info("发送大模型请求", utils.LogFields{
		StockCode: req.StockCode,
		TopK:      req.TopK,
		RequestID: prepared.RequestID,
		Message:   fmt.Sprintf("消息数量: %d", len(prepared.Messages)),
	})
	if guardedAnswer, guarded := ragchain.BuildGuardedAnswer(prepared.Request, prepared.Chunks); guarded {
		if onChunk != nil {
			if err := onChunk(guardedAnswer); err != nil {
				return appmodel.RAGQueryResponse{}, err
			}
		}
		return appmodel.RAGQueryResponse{
			Answer:         guardedAnswer,
			Citations:      prepared.Citations,
			RetrievedCount: prepared.RetrievedCount,
			RequestID:      prepared.RequestID,
		}, nil
	}
	answer, err := s.llmClient.StreamGenerate(ctx, llmReq, prepared.Citations)
	llmElapsed := time.Since(llmStart)

	if err != nil {
		utils.Error("大模型调用失败", utils.LogFields{
			StockCode: req.StockCode,
			TopK:      req.TopK,
			Elapsed:   llmElapsed,
			RequestID: prepared.RequestID,
			Message:   err.Error(),
		})
		return appmodel.RAGQueryResponse{}, err
	}
	answer = ragchain.ApplyAnswerGuard(prepared.Request, prepared.Chunks, answer)

	totalElapsed := time.Since(start)
	utils.Info("大模型调用完成", utils.LogFields{
		StockCode: req.StockCode,
		TopK:      req.TopK,
		Elapsed:   llmElapsed,
		RequestID: prepared.RequestID,
		Message:   fmt.Sprintf("生成答案长度: %d", len(answer)),
	})

	utils.Info("流式RAG查询全部完成", utils.LogFields{
		StockCode:      req.StockCode,
		TopK:           req.TopK,
		Elapsed:        totalElapsed,
		RetrievedCount: len(prepared.Citations),
		RequestID:      prepared.RequestID,
		Message:        fmt.Sprintf("生成答案长度: %d", len(answer)),
	})

	return appmodel.RAGQueryResponse{
		Answer:         answer,
		Citations:      prepared.Citations,
		RetrievedCount: prepared.RetrievedCount,
		RequestID:      prepared.RequestID,
	}, nil
}

// Pipeline 返回当前服务串联的关键阶段名。
func (s *QueryService) Pipeline() []string {
	_ = s
	steps := ragchain.DefaultRAGFlow()
	pipeline := make([]string, 0, len(steps))
	for _, step := range steps {
		pipeline = append(pipeline, step.Name)
	}
	return pipeline
}

// loadStockCodeMappings 从数据库中加载股票代码和公司名称的映射
func (s *QueryService) loadStockCodeMappings(ctx context.Context) error {
	if s.repo == nil {
		return fmt.Errorf("document repository unavailable")
	}

	docs, err := s.repo.ListDocuments(ctx)
	if err != nil {
		return fmt.Errorf("list documents: %w", err)
	}

	// 清空现有映射
	s.companyToCode = make(map[string]string)
	s.codeToCompany = make(map[string]string)

	// 从文档中提取映射
	for _, doc := range docs {
		if doc.StockCode != "" && doc.CompanyName != "" {
			// 添加公司名称到股票代码的映射
			s.companyToCode[doc.CompanyName] = doc.StockCode
			// 添加股票代码到公司名称的映射
			s.codeToCompany[doc.StockCode] = doc.CompanyName
		}
	}

	// 添加一些常见的别名
	for company, code := range s.companyToCode {
		// 添加公司名称的简称作为别名
		if len(company) > 2 {
			s.companyToCode[company[:2]] = code
		}
		// 添加小写版本作为别名
		s.companyToCode[strings.ToLower(company)] = code
	}

	utils.Info("Loaded stock code mappings", utils.LogFields{
		Message: fmt.Sprintf("for %d companies", len(s.codeToCompany)),
	})
	return nil
}

// truncateString 截断字符串，避免日志过长
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// RetrieveEvidence 仅执行检索操作
func (s *QueryService) RetrieveEvidence(ctx context.Context, req appmodel.RAGQueryRequest) ([]ragretriever.RetrievedChunk, error) {
	if req.StockCode == "" {
		req.StockCode = s.extractStockCode(req.Question)
	}
	chunks, err := s.retriever.Retrieve(ctx, req)
	if err != nil {
		return nil, err
	}
	return chunks, nil
}
