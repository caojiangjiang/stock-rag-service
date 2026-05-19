package concurrency

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cloudwego/eino/schema"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	einomodel "stock_rag/internal/eino/model"
	appmodel "stock_rag/internal/model"
)

var tracer = otel.Tracer("stock_rag/llm_client")

// LLMRequest 统一的 LLM 请求对象
type LLMRequest struct {
	RequestID string             // 请求唯一标识
	Question  string             // 用户问题/请求内容（用于构造 RAGQueryRequest）
	Messages  []*schema.Message  // 消息列表
	TaskType  string             // 任务类型：agent, rag, stream 等（用于分流、监控、降级）
	Priority  int                // 优先级：0-低, 1-中, 2-高
	Timeout   time.Duration      // 超时时间
	Stream    bool               // 是否为流式请求
	Metadata  map[string]string  // 元数据，用于监控和追踪
	OnChunk   func(string) error // 流式回调函数
}

// LLMClient 统一模型调用客户端
type LLMClient struct {
	chatModel    *einomodel.ChatModel
	queueManager *QueueManager
}

// NewLLMClient 创建模型调用客户端
func NewLLMClient(chatModel *einomodel.ChatModel, maxQueueSize, maxConcurrency int) *LLMClient {
	return NewLLMClientWithMaxWait(chatModel, maxQueueSize, maxConcurrency, 0)
}

// NewLLMClientWithMaxWait 创建模型调用客户端，支持最大等待时长
func NewLLMClientWithMaxWait(chatModel *einomodel.ChatModel, maxQueueSize, maxConcurrency int, maxWaitTime time.Duration) *LLMClient {
	qm := NewQueueManagerWithMaxWait(maxQueueSize, maxConcurrency, maxWaitTime)
	client := &LLMClient{
		chatModel:    chatModel,
		queueManager: qm,
	}

	// 启动队列管理器
	qm.Start(client.handleRequest)
	return client
}

// handleRequest 处理请求（支持流式和非流式）
func (c *LLMClient) handleRequest(req *Request) (string, error) {
	// 判断是否为流式请求
	if req.OnChunk != nil {
		// 流式请求
		return c.handleStreamRequest(req)
	}
	// 非流式请求
	return c.handleNonStreamRequest(req)
}

// handleNonStreamRequest 处理非流式请求
func (c *LLMClient) handleNonStreamRequest(req *Request) (string, error) {
	startTime := time.Now()
	
	// 创建 OpenTelemetry span
	ctx, span := tracer.Start(req.Ctx, "LLMClient.handleNonStreamRequest",
		trace.WithAttributes(
			attribute.String("request.id", req.ID),
			attribute.String("request.task_type", req.TaskType),
			attribute.Int("request.priority", req.Priority),
			attribute.String("request.question", truncateString(req.Question, 100)),
		))
	defer span.End()

	// 记录开始时间
	span.SetAttributes(attribute.Int64("start_time", startTime.UnixNano()))

	// 调用模型
	response, err := c.chatModel.Generate(ctx, appmodel.RAGQueryRequest{Question: req.Question}, req.Messages, nil)
	
	// 计算延迟
	latency := time.Since(startTime)
	
	// 设置 span 属性
	span.SetAttributes(
		attribute.Int64("latency_ms", latency.Milliseconds()),
		attribute.Int("response_length", len(response)),
	)

	if err != nil {
		log.Printf("[RAG流程] 大模型调用失败: %v", err)
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(attribute.String("error", err.Error()))
		return "", err
	}

	// 打印大模型返回值
	log.Printf("[RAG流程] 大模型返回值: %s", truncateString(response, 200))
	
	// 记录成功状态
	span.SetStatus(codes.Ok, "LLM call successful")
	
	// 模拟 token 计数（实际应从 API 响应中获取）
	tokenIn := estimateTokenCount(req.Question)
	tokenOut := estimateTokenCount(response)
	
	// 设置可观测性属性
	span.SetAttributes(
		attribute.Int("token_in", tokenIn),
		attribute.Int("token_out", tokenOut),
		attribute.Float64("cost_usd", calculateCost(tokenIn, tokenOut)),
		attribute.String("model_version", "unknown"),
	)

	// 记录 trace/span ID
	traceID := span.SpanContext().TraceID().String()
	spanID := span.SpanContext().SpanID().String()
	
	log.Printf("[LLM Trace] request=%s, trace_id=%s, span_id=%s, latency=%dms, token_in=%d, token_out=%d",
		req.ID, traceID, spanID, latency.Milliseconds(), tokenIn, tokenOut)

	return response, nil
}

// handleStreamRequest 处理流式请求
func (c *LLMClient) handleStreamRequest(req *Request) (string, error) {
	startTime := time.Now()
	
	// 创建 OpenTelemetry span
	ctx, span := tracer.Start(req.Ctx, "LLMClient.handleStreamRequest",
		trace.WithAttributes(
			attribute.String("request.id", req.ID),
			attribute.String("request.task_type", req.TaskType),
			attribute.Int("request.priority", req.Priority),
			attribute.String("request.question", truncateString(req.Question, 100)),
			attribute.Bool("request.stream", true),
		))
	defer span.End()

	// 记录开始时间
	span.SetAttributes(attribute.Int64("start_time", startTime.UnixNano()))

	// 调用流式生成
	radReq := appmodel.RAGQueryRequest{Question: req.Question}
	response, err := c.chatModel.StreamGenerate(ctx, radReq, req.Messages, nil, req.OnChunk)
	
	// 计算延迟
	latency := time.Since(startTime)
	
	// 设置 span 属性
	span.SetAttributes(
		attribute.Int64("latency_ms", latency.Milliseconds()),
		attribute.Int("response_length", len(response)),
	)

	if err != nil {
		log.Printf("[RAG流程] 大模型流式调用失败: %v", err)
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(attribute.String("error", err.Error()))
		return "", err
	}

	// 打印大模型返回值
	log.Printf("[RAG流程] 大模型流式返回值: %s", truncateString(response, 200))
	
	// 记录成功状态
	span.SetStatus(codes.Ok, "LLM stream call successful")
	
	// 模拟 token 计数
	tokenIn := estimateTokenCount(req.Question)
	tokenOut := estimateTokenCount(response)
	
	// 设置可观测性属性
	span.SetAttributes(
		attribute.Int("token_in", tokenIn),
		attribute.Int("token_out", tokenOut),
		attribute.Float64("cost_usd", calculateCost(tokenIn, tokenOut)),
		attribute.String("model_version", "unknown"),
	)

	// 记录 trace/span ID
	traceID := span.SpanContext().TraceID().String()
	spanID := span.SpanContext().SpanID().String()
	
	log.Printf("[LLM Trace] request=%s, trace_id=%s, span_id=%s, latency=%dms, token_in=%d, token_out=%d (streaming)",
		req.ID, traceID, spanID, latency.Milliseconds(), tokenIn, tokenOut)

	return response, nil
}

// Generate 生成响应（使用 LLMRequest 对象）
func (c *LLMClient) Generate(ctx context.Context, llmReq *LLMRequest) (string, error) {
	// 设置默认超时
	timeout := llmReq.Timeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}

	// 创建带超时的上下文
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 生成请求 ID
	requestID := llmReq.RequestID
	if requestID == "" {
		requestID = fmt.Sprintf("req-%d", time.Now().UnixNano())
	}

	// 创建内部请求
	req := &Request{
		ID:         requestID,
		Question:   llmReq.Question,
		TaskType:   llmReq.TaskType,
		Messages:   llmReq.Messages,
		Priority:   llmReq.Priority,
		CreatedAt:  time.Now(),
		Timeout:    timeout,
		Ctx:        reqCtx,
		ResponseCh: make(chan Response, 1),
		OnChunk:    llmReq.OnChunk,
	}

	// 提交请求
	err := c.queueManager.Submit(req)
	if err != nil {
		return "", err
	}

	// 等待结果
	select {
	case resp := <-req.ResponseCh:
		return resp.Result, resp.Error
	case <-reqCtx.Done():
		return "", reqCtx.Err()
	}
}

// StreamGenerate 流式生成响应（使用 LLMRequest 对象）
func (c *LLMClient) StreamGenerate(ctx context.Context, llmReq *LLMRequest, citations []appmodel.Citation) (string, error) {
	// 设置默认超时
	timeout := llmReq.Timeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}

	// 创建带超时的上下文
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 生成请求 ID
	requestID := llmReq.RequestID
	if requestID == "" {
		requestID = fmt.Sprintf("stream-req-%d", time.Now().UnixNano())
	}

	// 创建结果通道
	resultCh := make(chan Response, 1)

	// 创建请求
	request := &Request{
		ID:         requestID,
		Question:   llmReq.Question,
		TaskType:   llmReq.TaskType,
		Messages:   llmReq.Messages,
		Priority:   llmReq.Priority,
		CreatedAt:  time.Now(),
		Timeout:    timeout,
		Ctx:        reqCtx,
		ResponseCh: resultCh,
		OnChunk:    llmReq.OnChunk,
	}

	// 提交请求到队列
	err := c.queueManager.Submit(request)
	if err != nil {
		return "", err
	}

	// 等待最终结果
	select {
	case resp := <-resultCh:
		return resp.Result, resp.Error
	case <-reqCtx.Done():
		return "", reqCtx.Err()
	}
}

// GetStats 获取状态
func (c *LLMClient) GetStats() map[string]interface{} {
	return c.queueManager.GetStats()
}

// GetChatModel 获取 ChatModel
func (c *LLMClient) GetChatModel() *einomodel.ChatModel {
	return c.chatModel
}

// Close 关闭 LLMClient
func (c *LLMClient) Close() {
	c.queueManager.Stop()
}

// truncateString 截断字符串，避免日志过长
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// estimateTokenCount 估算 token 数量（实际应使用模型的 tokenizer）
func estimateTokenCount(text string) int {
	// 简单估算：1 token ≈ 4 个字符
	return len(text) / 4
}

// calculateCost 计算成本（模拟）
func calculateCost(tokenIn, tokenOut int) float64 {
	// 模拟成本计算：输入 $0.001 / 1k tokens，输出 $0.003 / 1k tokens
	inputCost := float64(tokenIn) * 0.001 / 1000
	outputCost := float64(tokenOut) * 0.003 / 1000
	return inputCost + outputCost
}
