package prompt

import (
	"context"
	"fmt"
	"log"
	"strings"

	einoprompt "github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/schema"

	appretriever "stock_rag/internal/eino/retriever"
	appmodel "stock_rag/internal/model"
)

// RAGPromptTemplate 是第一版问答 Prompt 的系统提示。
const RAGPromptTemplate = "你是股票投研问答助手。只能基于给定资料回答；如果证据不足，就明确说明。"

// PromptRules 返回第一版 Prompt 约束清单。
func PromptRules() []string {
	return []string{
		"仅基于检索资料回答",
		"给出引用来源",
		"不要编造不存在的事实",
		"若用户指定年份或指标，只能使用同年份、同指标证据；否则明确说明证据不足",
		"若上下文已明确给出所问指标及其数值，应直接引用该值，不要误判为证据不足",
		"若已能回答核心事实，但更细节未披露，可补充边界说明；不要把整题表述成无法回答",
		"不要直接给出投资建议",
	}
}

// NewRAGTemplate 返回一个基于 Eino prompt 组件的聊天模板。
func NewRAGTemplate() *einoprompt.DefaultChatTemplate {
	return einoprompt.FromMessages(
		schema.FString,
		schema.SystemMessage(RAGPromptTemplate+"\n规则：\n{rules}"),
		schema.UserMessage("问题：{question}\n股票：{stock_code}\n时间范围：{time_range}\n文档类型：{doc_types}\n解析约束：{query_constraints}\n检索上下文：\n{context}"),
	)
}

// BuildPromptVars 将请求与检索结果整理成模板变量。
func BuildPromptVars(req appmodel.RAGQueryRequest, chunks []appretriever.RetrievedChunk) map[string]any {
	contextLines := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		contextLines = append(contextLines, fmt.Sprintf("[%d] %s", i+1, chunk.Content))
	}

	docTypes := req.DocTypes
	if len(docTypes) == 0 {
		docTypes = appretriever.DefaultQueryOption().DocTypes
	}

	stockCode := req.StockCode
	if stockCode == "" {
		stockCode = "未指定"
	}

	timeRange := req.TimeRange
	if timeRange == "" {
		timeRange = "未指定"
	}

	intent := appretriever.AnalyzeQuery(req)
	queryConstraints := "未识别到显式年份/指标约束"
	constraintParts := make([]string, 0, 2)
	if intent.FiscalYear > 0 {
		constraintParts = append(constraintParts, fmt.Sprintf("年份=%d", intent.FiscalYear))
	}
	if intent.Metric != "" {
		metricConstraint := fmt.Sprintf("指标=%s", intent.Metric)
		if originalMetric := metricPhraseFromQuestion(strings.TrimSpace(req.Question), intent); originalMetric != "" && originalMetric != intent.Metric {
			metricConstraint = fmt.Sprintf("%s（原问法=%s）", metricConstraint, originalMetric)
		}
		constraintParts = append(constraintParts, metricConstraint)
	}
	if len(constraintParts) > 0 {
		queryConstraints = strings.Join(constraintParts, ", ")
	}

	return map[string]any{
		"rules":             "- " + strings.Join(PromptRules(), "\n- "),
		"question":          strings.TrimSpace(req.Question),
		"stock_code":        stockCode,
		"time_range":        timeRange,
		"doc_types":         strings.Join(docTypes, ", "),
		"query_constraints": queryConstraints,
		"context":           strings.Join(contextLines, "\n"),
	}
}

// BuildMessages 生成最终送入 ChatModel 的消息列表。
func BuildMessages(ctx context.Context, req appmodel.RAGQueryRequest, chunks []appretriever.RetrievedChunk) ([]*schema.Message, error) {
	log.Printf("[RAG流程] 开始构建大模型请求...")

	// 构建模板变量
	vars := BuildPromptVars(req, chunks)

	// 打印构建的上下文信息
	log.Printf("[RAG流程] 构建的模板变量：")
	log.Printf("[RAG流程] - 股票代码: %s", vars["stock_code"])
	log.Printf("[RAG流程] - 时间范围: %s", vars["time_range"])
	log.Printf("[RAG流程] - 文档类型: %s", vars["doc_types"])
	log.Printf("[RAG流程] - 解析约束: %s", vars["query_constraints"])
	log.Printf("[RAG流程] - 检索到的上下文片段数: %d", len(chunks))

	// 打印前几个上下文片段的摘要
	log.Printf("[RAG流程] 前3个上下文片段摘要：")
	for i, chunk := range chunks[:min(3, len(chunks))] {
		summary := chunk.Content
		if len(summary) > 100 {
			summary = summary[:100] + "..."
		}
		log.Printf("[RAG流程] 片段 %d: %s", i+1, summary)
	}

	// 生成消息
	messages, err := NewRAGTemplate().Format(ctx, vars)
	if err != nil {
		log.Printf("[RAG流程] 构建消息失败: %v", err)
		return nil, err
	}

	// 打印消息数量和类型
	log.Printf("[RAG流程] 构建完成，消息数量: %d", len(messages))
	for i, msg := range messages {
		log.Printf("[RAG流程] 消息 %d 类型: %s", i+1, msg.Role)
		if i == 0 && len(msg.Content) > 200 {
			// 打印系统消息的前200个字符
			log.Printf("[RAG流程] 系统消息内容: %s...", msg.Content[:200])
		}
	}

	return messages, nil
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func metricPhraseFromQuestion(question string, intent appretriever.QueryIntent) string {
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
	return best
}
