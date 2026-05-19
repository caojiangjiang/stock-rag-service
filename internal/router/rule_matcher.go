package router

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

type Rule struct {
	Name          string
	Mode          RouteMode
	Keywords      []string
	RegexPatterns []string
	Confidence    float64
	Reason        string
}

type HardRuleMatcher struct {
	rules []Rule
}

func NewHardRuleMatcher() *HardRuleMatcher {
	return &HardRuleMatcher{
		rules: []Rule{
			{
				Name:       "rag_keywords",
				Mode:       ModeRAG,
				Keywords:   []string{"研报", "公告", "财报", "文档", "原文", "引用", "根据资料", "根据报告", "基于知识", "参考文档"},
				Confidence: 0.95,
				Reason:     "命中RAG关键词",
			},
			{
				Name:       "analysis_keywords",
				Mode:       ModeAnalysis,
				Keywords:   []string{"分析", "提取指标", "总结观点", "估值", "同比", "环比", "财务指标", "拆解", "变化", "趋势"},
				Confidence: 0.9,
				Reason:     "命中分析关键词",
			},
			{
				Name:       "agent_complex_task",
				Mode:       ModeAgent,
				Keywords:   []string{"对比", "分步骤", "综合", "总结原因", "结合公告", "结合年报", "结合新闻", "综合分析", "多维度", "多角度", "交叉验证"},
				Confidence: 0.9,
				Reason:     "复杂任务型问题，需要多步骤处理",
			},
			{
				Name:       "agent_multi_object",
				Mode:       ModeAgent,
				Keywords:   []string{"和", "与", "vs", "以及", "及", "多个", "两家", "两家公司", "三家", "对比分析"},
				Confidence: 0.85,
				Reason:     "涉及多个对象对比",
			},
			{
				Name:       "agent_multi_time",
				Mode:       ModeAgent,
				Keywords:   []string{"2023年和2024年", "去年和今年", "同比", "环比", "季度对比", "年度对比", "多个年份", "时间序列"},
				Confidence: 0.85,
				Reason:     "涉及多个时间范围",
			},
			{
				Name:       "agent_multi_tool",
				Mode:       ModeAgent,
				Keywords:   []string{"帮我执行", "查多个", "连续步骤", "先查再比较", "多步", "工具调用", "规划", "任务", "先", "然后", "再", "最后"},
				Confidence: 0.85,
				Reason:     "需要多个工具协同",
			},
			{
				Name:       "chat_keywords",
				Mode:       ModeChat,
				Keywords:   []string{"你是谁", "你能做什么", "怎么使用", "介绍", "功能", "帮助", "闲聊", "你好", "谢谢"},
				Confidence: 0.95,
				Reason:     "命中Chat关键词",
			},
			{
				Name:       "rag_question_pattern",
				Mode:       ModeRAG,
				Keywords:   []string{"怎么样", "如何", "什么", "哪个", "多少", "哪些", "何时", "为何"},
				Confidence: 0.7,
				Reason:     "事实性问题，优先RAG",
			},
		},
	}
}

func (m *HardRuleMatcher) Match(input *RouteInput) ([]RuleMatch, error) {
	var matches []RuleMatch
	text := normalizeText(input.CurrentMessage)

	for _, rule := range m.rules {
		for _, keyword := range rule.Keywords {
			if strings.Contains(text, normalizeText(keyword)) {
				matches = append(matches, RuleMatch{
					Mode:       rule.Mode,
					Confidence: rule.Confidence,
					Reason:     rule.Reason + ": " + keyword,
					RuleName:   rule.Name,
				})
				break
			}
		}
	}

	return matches, nil
}

func normalizeText(text string) string {
	var builder strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(unicode.ToLower(r))
		}
	}
	return builder.String()
}

type LLMRouterClassifier struct {
	llmClient LLMClient
	prompt    string
}

type LLMClient interface {
	Completion(ctx context.Context, messages []LLMMessage) (string, error)
}

type LLMMessage struct {
	Role    string
	Content string
}

func NewLLMRouterClassifier(llmClient LLMClient) *LLMRouterClassifier {
	prompt := `你是一个智能路由分类器，请根据用户对话内容判断应该使用哪种模式回答。

可用模式：
1. chat - 闲聊、系统说明、不依赖资料检索的问题
2. rag - 需要基于文档/公告/研报/知识库回答，用户问事实、结论、出处
3. analysis - 基于检索结果做结构化分析，强调总结、比较、抽取、归纳
4. agent - 多步骤任务、多工具调用、需要规划执行

请按以下JSON格式输出：
{
    "mode": "模式名称",
    "confidence": 0.0-1.0,
    "reason": "分类理由",
    "candidates": [
        {"mode": "候选模式1", "confidence": 置信度},
        {"mode": "候选模式2", "confidence": 置信度}
    ]
}

约束条件：
- 如果需要文档依据，优先rag或analysis
- 如果需要多步骤工具，才选agent
- 如果仅是开放交流，不需要外部资料，选chat

对话上下文：
{{.Summary}}

最近对话：
{{.RecentMessages}}

当前消息：
{{.CurrentMessage}}

上一轮模式：{{.LastRouteMode}}

请输出JSON结果：`

	return &LLMRouterClassifier{
		llmClient: llmClient,
		prompt:    prompt,
	}
}

func (c *LLMRouterClassifier) Classify(ctx context.Context, input *RouteInput) (*LLMClassificationResult, error) {
	prompt := c.renderPrompt(input)

	messages := []LLMMessage{
		{Role: "user", Content: prompt},
	}

	response, err := c.llmClient.Completion(ctx, messages)
	if err != nil {
		return nil, err
	}

	return parseLLMResponse(response)
}

func (c *LLMRouterClassifier) renderPrompt(input *RouteInput) string {
	prompt := c.prompt
	prompt = strings.Replace(prompt, "{{.Summary}}", input.Summary, 1)

	var recentMsgs strings.Builder
	for i, msg := range input.RecentMessages {
		role := "用户"
		if msg.Role == "assistant" {
			role = "助手"
		}
		recentMsgs.WriteString(fmt.Sprintf("%d. %s: %s\n", i+1, role, msg.Content))
	}
	prompt = strings.Replace(prompt, "{{.RecentMessages}}", recentMsgs.String(), 1)

	prompt = strings.Replace(prompt, "{{.CurrentMessage}}", input.CurrentMessage, 1)
	prompt = strings.Replace(prompt, "{{.LastRouteMode}}", string(input.LastRouteMode), 1)

	return prompt
}

func parseLLMResponse(response string) (*LLMClassificationResult, error) {
	var result struct {
		Mode       string               `json:"mode"`
		Confidence float64              `json:"confidence"`
		Reason     string               `json:"reason"`
		Candidates []map[string]float64 `json:"candidates"`
	}

	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return &LLMClassificationResult{
			Mode:       ModeChat,
			Confidence: 0.5,
			Reason:     "解析失败，使用默认模式",
			Candidates: []CandidateMode{},
		}, nil
	}

	var candidates []CandidateMode
	for _, c := range result.Candidates {
		for modeStr, conf := range c {
			candidates = append(candidates, CandidateMode{
				Mode:       RouteMode(modeStr),
				Confidence: conf,
			})
		}
	}

	return &LLMClassificationResult{
		Mode:       RouteMode(result.Mode),
		Confidence: result.Confidence,
		Reason:     result.Reason,
		Candidates: candidates,
	}, nil
}
