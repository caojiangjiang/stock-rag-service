package agent

import (
	"context"
	"fmt"
	"strings"

	"stock_rag/internal/concurrency"
	"stock_rag/internal/router"
	"stock_rag/internal/service"
	"stock_rag/internal/pkgctx"
	"stock_rag/internal/utils"

	"github.com/cloudwego/eino/schema"
)

type RAGExecutor struct {
	name      string
	llmClient *concurrency.LLMClient
	retriever Retriever
}

type Retriever interface {
	Retrieve(ctx context.Context, query string, filters *RetrieveFilters) ([]RetrieveResult, error)
}

type RetrieveFilters struct {
	StockCode string
	DocType   string
	TimeRange string
}

type RetrieveResult struct {
	Content   string
	StockCode string
	DocType   string
	Title     string
	Score     float64
}

func NewRAGExecutor(llmClient *concurrency.LLMClient, retriever Retriever) *RAGExecutor {
	return &RAGExecutor{
		name:      "rag_executor",
		llmClient: llmClient,
		retriever: retriever,
	}
}

func (e *RAGExecutor) Name() string {
	return e.name
}

func (e *RAGExecutor) Mode() router.RouteMode {
	return router.ModeRAG
}

func (e *RAGExecutor) Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error) {
	stockCode := req.StockCode
	if stockCode == "" {
		taskCtx := &pkgctx.TaskContext{}
		service.NormalizeEntity(req.UserMessage, taskCtx)
		if taskCtx.StockCode != "" {
			stockCode = taskCtx.StockCode
		}
	}

	filters := &RetrieveFilters{
		StockCode: stockCode,
		DocType:   req.DocType,
		TimeRange: req.TimeRange,
	}

	results, err := e.retriever.Retrieve(ctx, req.UserMessage, filters)
	if err != nil {
		return &ExecuteResponse{
			Error: "检索失败: " + err.Error(),
		}, err
	}

	if len(results) == 0 {
		return &ExecuteResponse{
			Content: "未检索到相关资料，无法回答您的问题。",
			Mode:    router.ModeRAG,
		}, nil
	}

	// 只取前2条传递给大模型，减少token消耗
	maxForLLM := 2
	maxContentLen := 1500
	if len(results) < maxForLLM {
		maxForLLM = len(results)
	}

	var citations []Citation
	var contextBuilder strings.Builder
	totalContextLen := 0
	for i, result := range results[:maxForLLM] {
		content := result.Content
		if len(content) > maxContentLen {
			content = content[:maxContentLen] + "..."
		}
		totalContextLen += len(content)
		contextBuilder.WriteString("\n\n参考文档" + string(rune('1'+i)) + "：\n")
		contextBuilder.WriteString(content)
		citations = append(citations, Citation{
			StockCode: result.StockCode,
			DocType:   result.DocType,
			Title:     result.Title,
			Content:   result.Content,
			Score:     result.Score,
		})
	}

	systemPrompt := buildRAGPrompt(req.UserMessage, contextBuilder.String())

	messages := []*schema.Message{
		{
			Role:    "system",
			Content: systemPrompt,
		},
		{
			Role:    "user",
			Content: req.UserMessage,
		},
	}

	utils.Info("RAG调用LLM", utils.LogFields{
		Message: fmt.Sprintf("question_len=%d, context_len=%d, prompt_len=%d, citations=%d",
			len(req.UserMessage), totalContextLen, len(systemPrompt), len(citations)),
	})

	content, err := e.llmClient.Generate(ctx, &concurrency.LLMRequest{
		Question: req.UserMessage,
		TaskType: "rag",
		Stream:   false,
		Messages: messages,
	})
	if err != nil {
		return &ExecuteResponse{
			Error: err.Error(),
		}, err
	}

	return &ExecuteResponse{
		Content:   content,
		Mode:      router.ModeRAG,
		Citations: citations,
	}, nil
}

func buildRAGPrompt(question, context string) string {
	return `你是一个专业的金融助手。请根据以下参考文档回答用户的问题。

参考文档：
` + context + `

用户问题：` + question + `

请基于参考文档回答，如果文档中没有相关信息，请明确说明。`
}

type AnalysisExecutor struct {
	name      string
	llmClient *concurrency.LLMClient
	retriever Retriever
}

func NewAnalysisExecutor(llmClient *concurrency.LLMClient, retriever Retriever) *AnalysisExecutor {
	return &AnalysisExecutor{
		name:      "analysis_executor",
		llmClient: llmClient,
		retriever: retriever,
	}
}

func (e *AnalysisExecutor) Name() string {
	return e.name
}

func (e *AnalysisExecutor) Mode() router.RouteMode {
	return router.ModeAnalysis
}

func (e *AnalysisExecutor) Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error) {
	stockCode := req.StockCode
	if stockCode == "" {
		taskCtx := &pkgctx.TaskContext{}
		service.NormalizeEntity(req.UserMessage, taskCtx)
		if taskCtx.StockCode != "" {
			stockCode = taskCtx.StockCode
		}
	}

	filters := &RetrieveFilters{
		StockCode: stockCode,
		DocType:   req.DocType,
		TimeRange: req.TimeRange,
	}

	results, err := e.retriever.Retrieve(ctx, req.UserMessage, filters)
	if err != nil {
		return &ExecuteResponse{
			Error: "检索失败: " + err.Error(),
		}, err
	}

	// 只取前2条传递给大模型，减少token消耗
	maxForLLM := 2
	maxContentLen := 2000
	if len(results) < maxForLLM {
		maxForLLM = len(results)
	}

	var citations []Citation
	var contextBuilder strings.Builder
	for i, result := range results[:maxForLLM] {
		content := result.Content
		if len(content) > maxContentLen {
			content = content[:maxContentLen] + "..."
		}
		contextBuilder.WriteString("\n\n参考文档" + string(rune('1'+i)) + "：\n")
		contextBuilder.WriteString(content)
		citations = append(citations, Citation{
			StockCode: result.StockCode,
			DocType:   result.DocType,
			Title:     result.Title,
			Content:   result.Content,
			Score:     result.Score,
		})
	}

	systemPrompt := buildAnalysisPrompt(req.UserMessage, contextBuilder.String())

	messages := []*schema.Message{
		{
			Role:    "system",
			Content: systemPrompt,
		},
		{
			Role:    "user",
			Content: req.UserMessage,
		},
	}

	content, err := e.llmClient.Generate(ctx, &concurrency.LLMRequest{
		Question: req.UserMessage,
		TaskType: "analysis",
		Stream:   false,
		Messages: messages,
	})
	if err != nil {
		return &ExecuteResponse{
			Error: err.Error(),
		}, err
	}

	return &ExecuteResponse{
		Content:   content,
		Mode:      router.ModeAnalysis,
		Citations: citations,
	}, nil
}

func buildAnalysisPrompt(question, context string) string {
	return `你是一个专业的金融分析师。请对用户提供的问题进行深入分析。

参考文档：
` + context + `

分析任务：` + question + `

请从以下维度进行分析：
1. 关键数据提取和计算
2. 多维度对比分析
3. 趋势判断
4. 结论和建议

请给出结构化的分析结果。`
}
