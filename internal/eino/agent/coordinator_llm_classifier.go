package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"stock_rag/internal/concurrency"

	"github.com/cloudwego/eino/schema"
)

// CoordinatorLLMClassifierImpl 协调器 LLM 分类器实现
type CoordinatorLLMClassifierImpl struct {
	llmClient *concurrency.LLMClient
	prompt    string
}

// NewCoordinatorLLMClassifier 创建协调器 LLM 分类器
func NewCoordinatorLLMClassifier(llmClient *concurrency.LLMClient) *CoordinatorLLMClassifierImpl {
	prompt := `你是一个智能协调器分类器，请根据用户对话内容判断应该使用哪种协调器类型。

可用协调器类型：
1. supervisor - 单轮监督式任务，简单分析或工具调用
2. plan - 需要多步骤规划的复杂任务，如分步骤分析、多阶段处理
3. pipeline - 固定流程任务，按顺序执行多个步骤
4. workflow - 工作流模板任务，使用预设模板生成标准化输出
5. debate - 辩论式分析，需要正反方观点对比
6. committee - 合议式分析，综合多个独立分析结果
7. peer - 并行独立分析，多个分析师独立工作后协商
8. deep - 深度研究任务，需要全面详尽的分析

请按以下JSON格式输出：
{
    "type": "协调器类型",
    "confidence": 0.0-1.0,
    "reason": "分类理由",
    "candidates": [
        {"type": "候选类型1", "confidence": 置信度},
        {"type": "候选类型2", "confidence": 置信度}
    ]
}

约束条件：
- 需要分步骤完成的复杂任务选 plan
- 固定流程或标准流程选 pipeline
- 需要正反方辩论选 debate
- 需要多角度独立分析选 peer 或 committee
- 深度研究报告选 deep
- 模板化输出选 workflow
- 简单分析或工具调用选 supervisor

对话上下文摘要：
{{.Summary}}

最近对话：
{{.RecentMessages}}

当前消息：
{{.CurrentMessage}}

复杂度评分：{{.ComplexityScore}}

上一轮协调器：{{.LastCoordinator}}

请输出JSON结果：`

	return &CoordinatorLLMClassifierImpl{
		llmClient: llmClient,
		prompt:    prompt,
	}
}

// Classify 执行协调器分类
func (c *CoordinatorLLMClassifierImpl) Classify(ctx context.Context, input *CoordinatorSelectInput, complexity float64) (*CoordinatorLLMResult, error) {
	prompt := c.renderPrompt(input, complexity)

	messages := []*schema.Message{
		{
			Role:    "user",
			Content: prompt,
		},
	}

	response, err := c.llmClient.Generate(ctx, &concurrency.LLMRequest{
		Question: input.CurrentMessage,
		TaskType: "coordinator_classifier",
		Messages: messages,
	})
	if err != nil {
		return nil, err
	}

	return parseCoordinatorLLMResponse(response)
}

// renderPrompt 渲染分类提示词
func (c *CoordinatorLLMClassifierImpl) renderPrompt(input *CoordinatorSelectInput, complexity float64) string {
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
	prompt = strings.Replace(prompt, "{{.ComplexityScore}}", fmt.Sprintf("%.4f", complexity), 1)
	prompt = strings.Replace(prompt, "{{.LastCoordinator}}", string(input.LastCoordinator), 1)

	return prompt
}

// parseCoordinatorLLMResponse 解析 LLM 输出
func parseCoordinatorLLMResponse(response string) (*CoordinatorLLMResult, error) {
	type candidateItem struct {
		Type       string  `json:"type"`
		Confidence float64 `json:"confidence"`
	}

	var result struct {
		Type       string          `json:"type"`
		Confidence float64         `json:"confidence"`
		Reason     string          `json:"reason"`
		Candidates []candidateItem `json:"candidates"`
	}

	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return &CoordinatorLLMResult{
			Type:       CoordinatorTypeSupervisor,
			Confidence: 0.5,
			Reason:     "解析失败，使用默认协调器",
			Candidates: []CandidateCoordinator{},
		}, nil
	}

	// 验证置信度范围
	if result.Confidence < 0 || result.Confidence > 1 {
		result.Confidence = 0.5
		result.Reason = "置信度超出范围，已修正"
	}

	var candidates []CandidateCoordinator
	for _, cand := range result.Candidates {
		if cand.Confidence >= 0 && cand.Confidence <= 1 && cand.Type != "" {
			candidates = append(candidates, CandidateCoordinator{
				Type:       CoordinatorType(cand.Type),
				Confidence: cand.Confidence,
			})
		}
	}

	return &CoordinatorLLMResult{
		Type:       CoordinatorType(result.Type),
		Confidence: result.Confidence,
		Reason:     result.Reason,
		Candidates: candidates,
	}, nil
}
