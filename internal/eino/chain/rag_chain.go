package chain

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"stock_rag/internal/concurrency"
	einomodel "stock_rag/internal/eino/model"
	ragprompt "stock_rag/internal/eino/prompt"
	ragretriever "stock_rag/internal/eino/retriever"
	"stock_rag/internal/eino/trace"
	appmodel "stock_rag/internal/model"
)

var (
	answerSplitRE = regexp.MustCompile(`[。；\n]+`)
	metricValueRE = regexp.MustCompile(`[0-9][0-9,]*(?:\.[0-9]+)?(?:亿元|万亿元|万元|元|%|倍)?`)
	refusalHints  = []string{
		"证据不足",
		"无法回答",
		"无法直接回答",
		"未明确披露",
		"未披露",
		"未检索到",
		"信息不足",
		"暂无法提供",
		"无法提供",
	}
)

// FlowStep 表示第一版 RAG 主链路中的一个步骤。
type FlowStep struct {
	Name    string
	Purpose string
}

type flowState struct {
	Request  appmodel.RAGQueryRequest
	Chunks   []ragretriever.RetrievedChunk
	Messages []*schema.Message
	Answer   string
}

// PreparedQuery 是流式与非流式链路共用的查询准备结果。
type PreparedQuery struct {
	Request        appmodel.RAGQueryRequest
	Chunks         []ragretriever.RetrievedChunk
	Messages       []*schema.Message
	Citations      []appmodel.Citation
	RetrievedCount int
	RequestID      string
}

// DefaultRAGFlow 返回推荐的最小主链路。
func DefaultRAGFlow() []FlowStep {
	return []FlowStep{
		{Name: "validate_input", Purpose: "校验与标准化查询参数"},
		{Name: "retrieve_context", Purpose: "检索 chunk 与 citations"},
		{Name: "build_prompt", Purpose: "拼装最终 Prompt"},
		{Name: "generate_answer", Purpose: "调用 ChatModel 生成答案"},
		{Name: "answer_guard", Purpose: "校验年份/指标与证据一致性"},
		{Name: "format_output", Purpose: "整理 answer 和 citations"},
	}
}

// NewSkeletonRunner 创建一个基于 Eino compose 的最小可编译问答链。
func NewSkeletonRunner(ctx context.Context, chatModel *einomodel.ChatModel, retriever ragretriever.Retriever) (compose.Runnable[appmodel.RAGQueryRequest, appmodel.RAGQueryResponse], error) {
	if chatModel == nil {
		return nil, fmt.Errorf("chat model is nil")
	}
	if retriever == nil {
		return nil, fmt.Errorf("retriever is nil")
	}

	// 创建tracer
	tracer := trace.NewDefaultTracer()

	chain := compose.NewChain[appmodel.RAGQueryRequest, appmodel.RAGQueryResponse]()
	chain.
		AppendLambda(compose.InvokableLambda(func(ctx context.Context, req appmodel.RAGQueryRequest) (flowState, error) {
			ctx, done := tracer.Start(ctx, "chain", "validate_input", map[string]interface{}{
				"question":   req.Question,
				"stock_code": req.StockCode,
				"top_k":      req.TopK,
			})
			defer done(true, "")
			return flowState{Request: normalizeRequest(req)}, nil
		})).
		AppendLambda(compose.InvokableLambda(func(ctx context.Context, state flowState) (flowState, error) {
			start := time.Now()
			ctx, done := tracer.Start(ctx, "chain", "retrieve_context", map[string]interface{}{
				"stock_code": state.Request.StockCode,
				"top_k":      state.Request.TopK,
			})
			chunks, err := retriever.Retrieve(ctx, state.Request)
			latency := time.Since(start).Milliseconds()
			done(err == nil, "")
			if err != nil {
				return flowState{}, err
			}

			state.Chunks = chunks

			// 构建检索结果详情
			details := make(map[string]interface{})
			details["retrieved_count"] = len(chunks)
			details["latency_ms"] = latency
			details["question"] = state.Request.Question
			details["stock_code"] = state.Request.StockCode

			// 记录每个检索到的chunk信息
			for i, chunk := range chunks {
				content := chunk.Content
				if len(content) > 100 {
					content = content[:100] + "..."
				}
				details[fmt.Sprintf("chunk_%d_title", i)] = chunk.Citation.Title
				details[fmt.Sprintf("chunk_%d_content", i)] = content
				details[fmt.Sprintf("chunk_%d_doc_type", i)] = chunk.Citation.DocType
				details[fmt.Sprintf("chunk_%d_published", i)] = chunk.Citation.Published
			}

			tracer.Record(ctx, "chain", "retrieve_context_done", details)
			return state, nil
		})).
		AppendLambda(compose.InvokableLambda(func(ctx context.Context, state flowState) (flowState, error) {
			start := time.Now()
			ctx, done := tracer.Start(ctx, "chain", "build_prompt", map[string]interface{}{
				"chunk_count": len(state.Chunks),
			})
			messages, err := ragprompt.BuildMessages(ctx, state.Request, state.Chunks)
			latency := time.Since(start).Milliseconds()
			done(err == nil, "")
			if err != nil {
				return flowState{}, err
			}

			state.Messages = messages
			tracer.Record(ctx, "chain", "build_prompt_done", map[string]interface{}{
				"message_count": len(messages),
				"latency_ms":    latency,
			})
			return state, nil
		})).
		AppendLambda(compose.InvokableLambda(func(ctx context.Context, state flowState) (flowState, error) {
			start := time.Now()
			ctx, done := tracer.Start(ctx, "chain", "generate_answer", map[string]interface{}{
				"message_count": len(state.Messages),
			})
			answer, err := chatModel.Generate(ctx, state.Request, state.Messages, citationsFromChunks(state.Chunks))
			latency := time.Since(start).Milliseconds()
			done(err == nil, "")
			if err != nil {
				return flowState{}, err
			}
			state.Answer = answer
			tracer.Record(ctx, "chain", "generate_answer_done", map[string]interface{}{
				"answer_length": len(answer),
				"latency_ms":    latency,
			})
			return state, nil
		})).
		AppendLambda(compose.InvokableLambda(func(ctx context.Context, state flowState) (flowState, error) {
			ctx, done := tracer.Start(ctx, "chain", "answer_guard", map[string]interface{}{
				"citation_count": len(state.Chunks),
			})
			defer done(true, "")
			state.Answer = ApplyAnswerGuard(state.Request, state.Chunks, state.Answer)
			return state, nil
		})).
		AppendLambda(compose.InvokableLambda(func(ctx context.Context, state flowState) (appmodel.RAGQueryResponse, error) {
			start := time.Now()
			ctx, done := tracer.Start(ctx, "chain", "format_output", map[string]interface{}{
				"citation_count": len(state.Chunks),
			})
			defer done(true, "")
			response := appmodel.RAGQueryResponse{
				Answer:         state.Answer,
				Citations:      citationsFromChunks(state.Chunks),
				RetrievedCount: len(state.Chunks),
				RequestID:      buildRequestID(state.Request),
			}
			latency := time.Since(start).Milliseconds()
			tracer.Record(ctx, "chain", "format_output_done", map[string]interface{}{
				"citation_count": len(response.Citations),
				"latency_ms":     latency,
			})
			return response, nil
		}))

	return chain.Compile(ctx)
}

// NewSkeletonRunnerWithLLMClient 创建一个基于 Eino compose 的最小可编译问答链，使用 LLMClient 进行模型调用。
func NewSkeletonRunnerWithLLMClient(ctx context.Context, llmClient *concurrency.LLMClient, retriever ragretriever.Retriever) (compose.Runnable[appmodel.RAGQueryRequest, appmodel.RAGQueryResponse], error) {
	if llmClient == nil {
		return nil, fmt.Errorf("llm client is nil")
	}
	if retriever == nil {
		return nil, fmt.Errorf("retriever is nil")
	}

	// 创建tracer
	tracer := trace.NewDefaultTracer()

	chain := compose.NewChain[appmodel.RAGQueryRequest, appmodel.RAGQueryResponse]()
	chain.
		AppendLambda(compose.InvokableLambda(func(ctx context.Context, req appmodel.RAGQueryRequest) (flowState, error) {
			ctx, done := tracer.Start(ctx, "chain", "validate_input", map[string]interface{}{
				"question":   req.Question,
				"stock_code": req.StockCode,
				"top_k":      req.TopK,
			})
			defer done(true, "")
			return flowState{Request: normalizeRequest(req)}, nil
		})).
		AppendLambda(compose.InvokableLambda(func(ctx context.Context, state flowState) (flowState, error) {
			ctx, done := tracer.Start(ctx, "chain", "retrieve_context", map[string]interface{}{
				"stock_code": state.Request.StockCode,
				"top_k":      state.Request.TopK,
			})
			chunks, err := retriever.Retrieve(ctx, state.Request)
			done(err == nil, "")
			if err != nil {
				return flowState{}, err
			}

			state.Chunks = chunks
			tracer.Record(ctx, "chain", "retrieve_context_done", map[string]interface{}{
				"retrieved_count": len(chunks),
			})
			return state, nil
		})).
		AppendLambda(compose.InvokableLambda(func(ctx context.Context, state flowState) (flowState, error) {
			ctx, done := tracer.Start(ctx, "chain", "build_prompt", map[string]interface{}{
				"chunk_count": len(state.Chunks),
			})
			messages, err := ragprompt.BuildMessages(ctx, state.Request, state.Chunks)
			done(err == nil, "")
			if err != nil {
				return flowState{}, err
			}

			state.Messages = messages
			return state, nil
		})).
		AppendLambda(compose.InvokableLambda(func(ctx context.Context, state flowState) (flowState, error) {
			ctx, done := tracer.Start(ctx, "chain", "generate_answer", map[string]interface{}{
				"message_count": len(state.Messages),
			})
			llmReq := concurrency.LLMRequest{
				RequestID: fmt.Sprintf("rag-%d", time.Now().UnixNano()),
				Question:  state.Request.Question,
				Messages:  state.Messages,
				TaskType:  "rag",
				Priority:  0,
				Timeout:   2 * time.Minute,
				Stream:    false,
				Metadata: map[string]string{
					"question": state.Request.Question,
				},
			}
			answer, err := llmClient.Generate(ctx, &llmReq)
			done(err == nil, "")
			if err != nil {
				return flowState{}, err
			}
			state.Answer = answer
			tracer.Record(ctx, "chain", "generate_answer_done", map[string]interface{}{
				"answer_length": len(answer),
			})
			return state, nil
		})).
		AppendLambda(compose.InvokableLambda(func(ctx context.Context, state flowState) (flowState, error) {
			ctx, done := tracer.Start(ctx, "chain", "answer_guard", map[string]interface{}{
				"citation_count": len(state.Chunks),
			})
			defer done(true, "")
			state.Answer = ApplyAnswerGuard(state.Request, state.Chunks, state.Answer)
			return state, nil
		})).
		AppendLambda(compose.InvokableLambda(func(ctx context.Context, state flowState) (appmodel.RAGQueryResponse, error) {
			ctx, done := tracer.Start(ctx, "chain", "format_output", map[string]interface{}{
				"citation_count": len(state.Chunks),
			})
			defer done(true, "")
			response := appmodel.RAGQueryResponse{
				Answer:         state.Answer,
				Citations:      citationsFromChunks(state.Chunks),
				RetrievedCount: len(state.Chunks),
				RequestID:      buildRequestID(state.Request),
			}
			tracer.Record(ctx, "chain", "format_output_done", map[string]interface{}{
				"citation_count": len(response.Citations),
			})
			return response, nil
		}))

	return chain.Compile(ctx)
}

// PrepareQuery 复用当前 RAG 主链路中的前置步骤，为流式输出准备上下文。
func PrepareQuery(ctx context.Context, req appmodel.RAGQueryRequest, retriever ragretriever.Retriever) (PreparedQuery, error) {
	if retriever == nil {
		return PreparedQuery{}, fmt.Errorf("retriever is nil")
	}

	// 创建tracer
	tracer := trace.NewDefaultTracer()
	normalized := normalizeRequest(req)

	// 跟踪检索过程
	ctx, done := tracer.Start(ctx, "chain", "prepare_query_retrieve", map[string]interface{}{
		"stock_code": normalized.StockCode,
		"top_k":      normalized.TopK,
	})
	chunks, err := retriever.Retrieve(ctx, normalized)
	done(err == nil, "")
	if err != nil {
		return PreparedQuery{}, err
	}

	// 跟踪Prompt构建过程
	ctx, done = tracer.Start(ctx, "chain", "prepare_query_build_prompt", map[string]interface{}{
		"chunk_count": len(chunks),
	})
	messages, err := ragprompt.BuildMessages(ctx, normalized, chunks)
	done(err == nil, "")
	if err != nil {
		return PreparedQuery{}, err
	}

	citations := citationsFromChunks(chunks)
	tracer.Record(ctx, "chain", "prepare_query_done", map[string]interface{}{
		"retrieved_count": len(chunks),
		"citation_count":  len(citations),
	})
	return PreparedQuery{
		Request:        normalized,
		Chunks:         chunks,
		Messages:       messages,
		Citations:      citations,
		RetrievedCount: len(chunks),
		RequestID:      buildRequestID(normalized),
	}, nil
}

func normalizeRequest(req appmodel.RAGQueryRequest) appmodel.RAGQueryRequest {
	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" {
		req.Question = "请总结这只股票最近的重要信息"
	}

	if req.TopK <= 0 {
		req.TopK = ragretriever.DefaultQueryOption().TopK
	}

	if len(req.DocTypes) == 0 {
		req.DocTypes = ragretriever.DefaultQueryOption().DocTypes
	}

	return req
}

func citationsFromChunks(chunks []ragretriever.RetrievedChunk) []appmodel.Citation {
	// 使用 map 合并相同的文件
	citationMap := make(map[string]appmodel.Citation)
	for _, chunk := range chunks {
		// 获取现有的 citation（如果存在）
		citation, exists := citationMap[chunk.Citation.Title]
		if !exists {
			// 如果不存在，使用 chunk.Citation 作为基础
			citation = chunk.Citation
		}

		// 将 chunk 的内容添加到 citation 中
		if citation.Content == "" {
			citation.Content = chunk.Content
		} else {
			// 如果已经有内容，追加新内容（去重）
			if !strings.Contains(citation.Content, chunk.Content) {
				citation.Content += "\n\n" + chunk.Content
			}
		}

		// 更新 map
		citationMap[chunk.Citation.Title] = citation
	}

	// 将 map 转换为切片
	citations := make([]appmodel.Citation, 0, len(citationMap))
	for _, citation := range citationMap {
		citations = append(citations, citation)
	}

	return citations
}

func buildRequestID(req appmodel.RAGQueryRequest) string {
	stockCode := req.StockCode
	if stockCode == "" {
		stockCode = "general"
	}

	return fmt.Sprintf("rag-%s-%d", strings.ToLower(stockCode), len([]rune(req.Question)))
}

// BuildGuardedAnswer 在证据与问题年份/指标约束不一致时返回保守回答。
func BuildGuardedAnswer(req appmodel.RAGQueryRequest, chunks []ragretriever.RetrievedChunk) (string, bool) {
	intent := ragretriever.AnalyzeQuery(req)
	if !intent.HasConstraints() {
		return "", false
	}

	for _, chunk := range chunks {
		if intent.MatchesChunk(chunk) {
			return "", false
		}
	}
	return guardedAnswerForIntent(intent, len(chunks) > 0), true
}

func guardedAnswerForIntent(intent ragretriever.QueryIntent, hasRetrievedEvidence bool) string {
	requirements := make([]string, 0, 2)
	if intent.FiscalYear > 0 {
		requirements = append(requirements, fmt.Sprintf("%d年", intent.FiscalYear))
	}
	if intent.Metric != "" {
		requirements = append(requirements, intent.Metric)
	}

	target := strings.Join(requirements, "")
	if target == "" {
		target = "当前问题"
	}

	answer := fmt.Sprintf("当前检索证据不足，无法直接回答%s。", target)
	if hasRetrievedEvidence {
		answer += "现有检索结果未能同时满足用户要求的年份/指标约束，不能据此外推。"
	} else {
		answer += "当前没有检索到可直接支撑该约束的资料。"
	}
	answer += "请补充对应年度报告、业绩预告或相关公告后再回答。"
	return answer
}

// ApplyAnswerGuard 在生成回答后再次校验年份/指标一致性。
func ApplyAnswerGuard(req appmodel.RAGQueryRequest, chunks []ragretriever.RetrievedChunk, answer string) string {
	if guarded, ok := BuildGuardedAnswer(req, chunks); ok {
		return guarded
	}

	intent := ragretriever.AnalyzeQuery(req)
	recoveredAnswer, recoverable := buildEvidenceBackedAnswer(req, intent, chunks)
	if recoverable && answerSuggestsInsufficientEvidence(answer) {
		return recoveredAnswer
	}

	if intent.FiscalYear <= 0 {
		return polishBorderlineAnswer(answer, chunks)
	}

	answerYears := ragretriever.ExtractYears(answer)
	if len(answerYears) == 0 {
		return answer
	}
	for _, year := range answerYears {
		if year == intent.FiscalYear {
			return polishBorderlineAnswer(answer, chunks)
		}
	}
	if recoverable {
		return recoveredAnswer
	}
	return guardedAnswerForIntent(intent, len(chunks) > 0)
}

func buildEvidenceBackedAnswer(req appmodel.RAGQueryRequest, intent ragretriever.QueryIntent, chunks []ragretriever.RetrievedChunk) (string, bool) {
	if intent.Metric == "" || len(chunks) == 0 {
		return "", false
	}

	metricLabel := preferredMetricLabel(req.Question, intent)
	if metricLabel == "" {
		metricLabel = intent.Metric
	}
	searchTerms := metricSearchTerms(req.Question, intent)

	for _, chunk := range chunks {
		if !intent.MatchesChunk(chunk) {
			continue
		}
		value, matchedTerm := extractMetricValue(chunk.Content, searchTerms)
		if value == "" {
			continue
		}
		if metricLabel == intent.Metric && matchedTerm != "" {
			metricLabel = matchedTerm
		}

		if intent.FiscalYear > 0 {
			return fmt.Sprintf("根据检索到的资料，%d年%s为%s。\n引用来源：[1] %s", intent.FiscalYear, metricLabel, value, chunk.Citation.Title), true
		}
		return fmt.Sprintf("根据检索到的资料，%s为%s。\n引用来源：[1] %s", metricLabel, value, chunk.Citation.Title), true
	}

	return "", false
}

func answerSuggestsInsufficientEvidence(answer string) bool {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	for _, hint := range refusalHints {
		if strings.Contains(lower, strings.ToLower(hint)) {
			return true
		}
	}
	return false
}

func preferredMetricLabel(question string, intent ragretriever.QueryIntent) string {
	questionLower := strings.ToLower(strings.TrimSpace(question))
	best := ""
	for _, alias := range intent.MetricAliases {
		if alias == "" {
			continue
		}
		if strings.Contains(questionLower, strings.ToLower(alias)) && len([]rune(alias)) > len([]rune(best)) {
			best = alias
		}
	}
	if best != "" {
		return best
	}
	return intent.Metric
}

func metricSearchTerms(question string, intent ragretriever.QueryIntent) []string {
	questionLower := strings.ToLower(strings.TrimSpace(question))
	terms := make([]string, 0, len(intent.MetricAliases)+1)
	seen := make(map[string]struct{}, len(intent.MetricAliases)+1)
	add := func(term string) {
		term = strings.TrimSpace(term)
		if term == "" {
			return
		}
		key := strings.ToLower(term)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		terms = append(terms, term)
	}

	for _, alias := range intent.MetricAliases {
		if alias != "" && strings.Contains(questionLower, strings.ToLower(alias)) {
			add(alias)
		}
	}
	for _, alias := range intent.MetricAliases {
		add(alias)
	}
	add(intent.Metric)
	return terms
}

func extractMetricValue(content string, searchTerms []string) (string, string) {
	normalized := strings.NewReplacer(" ", "", "\t", "", "：", "", ":", "").Replace(content)
	normalizedLower := strings.ToLower(normalized)

	for _, term := range searchTerms {
		cleanTerm := strings.NewReplacer(" ", "", "\t", "", "：", "", ":", "").Replace(term)
		if cleanTerm == "" {
			continue
		}
		idx := strings.Index(normalizedLower, strings.ToLower(cleanTerm))
		if idx < 0 {
			continue
		}
		snippet := normalized[idx+len(cleanTerm):]
		runes := []rune(snippet)
		if len(runes) > 48 {
			snippet = string(runes[:48])
		}
		if match := metricValueRE.FindString(snippet); match != "" {
			return strings.ReplaceAll(match, ",", ""), term
		}
	}

	return "", ""
}

func polishBorderlineAnswer(answer string, chunks []ragretriever.RetrievedChunk) string {
	if !answerSuggestsInsufficientEvidence(answer) || len(chunks) == 0 {
		return answer
	}

	parts := answerSplitRE.Split(answer, -1)
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		segment := strings.TrimSpace(part)
		if segment == "" {
			continue
		}
		if strings.Contains(segment, "引用来源") {
			continue
		}
		if answerSuggestsInsufficientEvidence(segment) {
			segment = trimRefusalSuffix(segment)
			if segment == "" {
				continue
			}
		}
		kept = append(kept, segment)
	}
	if len(kept) == 0 {
		return answer
	}

	polished := strings.Join(kept, "。") + "。"
	if title := strings.TrimSpace(chunks[0].Citation.Title); title != "" {
		polished += "\n引用来源：[1] " + title
	}
	return polished
}

func trimRefusalSuffix(segment string) string {
	earliest := -1
	for _, hint := range refusalHints {
		idx := strings.Index(strings.ToLower(segment), strings.ToLower(hint))
		if idx < 0 {
			continue
		}
		if earliest == -1 || idx < earliest {
			earliest = idx
		}
	}
	if earliest < 0 {
		return strings.TrimSpace(segment)
	}
	prefix := strings.TrimSpace(segment[:earliest])
	prefix = strings.TrimRight(prefix, "，,:：；-—([【")
	prefix = strings.TrimSpace(prefix)
	return prefix
}
