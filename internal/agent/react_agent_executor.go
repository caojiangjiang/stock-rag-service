package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"go.opentelemetry.io/otel/attribute"

	"stock_rag/internal/concurrency"
	"stock_rag/internal/observability"
	"stock_rag/internal/router"
)

// Tool 定义工具接口
type Tool interface {
	Name() string
	Description() string
	Parameters() []ToolParameter
	Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
}

// ToolParameter 工具参数定义
type ToolParameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// ReActAgentConfig ReAct Agent 配置
type ReActAgentConfig struct {
	MaxSteps         int           // 最大思考步数
	MaxRetryAttempts int           // 单工具最大重试次数
	RetryDelay       time.Duration // 重试间隔
	Temperature      float64       // LLM 温度
	Verbose          bool          // 是否输出详细日志
}

// DefaultReActAgentConfig 返回默认配置
func DefaultReActAgentConfig() ReActAgentConfig {
	return ReActAgentConfig{
		MaxSteps:         10,
		MaxRetryAttempts: 3,
		RetryDelay:       2 * time.Second,
		Temperature:      0.7,
		Verbose:          true,
	}
}

// ReActAgentExecutor 实现 ReAct 循环的 Agent 执行器
type ReActAgentExecutor struct {
	name      string
	llmClient *concurrency.LLMClient
	tools     []Tool
	config    ReActAgentConfig
}

// NewReActAgentExecutor 创建 ReAct Agent 执行器
func NewReActAgentExecutor(llmClient *concurrency.LLMClient, tools []Tool, config ReActAgentExecutorConfig) *ReActAgentExecutor {
	agentConfig := DefaultReActAgentConfig()
	if config.MaxSteps > 0 {
		agentConfig.MaxSteps = config.MaxSteps
	}
	if config.MaxRetryAttempts > 0 {
		agentConfig.MaxRetryAttempts = config.MaxRetryAttempts
	}
	if config.RetryDelay > 0 {
		agentConfig.RetryDelay = config.RetryDelay
	}

	return &ReActAgentExecutor{
		name:      "react_agent_executor",
		llmClient: llmClient,
		tools:     tools,
		config:    agentConfig,
	}
}

// ReActAgentExecutorConfig 构造函数配置
type ReActAgentExecutorConfig struct {
	MaxSteps         int
	MaxRetryAttempts int
	RetryDelay       time.Duration
}

func (e *ReActAgentExecutor) Name() string {
	return e.name
}

func (e *ReActAgentExecutor) Mode() router.RouteMode {
	return router.ModeAgent
}

// AgentThought 代理思考步骤
type AgentThought struct {
	Step        int    `json:"step"`
	Thought     string `json:"thought"`
	Action      string `json:"action"`
	ActionInput string `json:"action_input"`
	Observation string `json:"observation"`
}

// Execute 执行 ReAct 循环
func (e *ReActAgentExecutor) Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error) {
	ctx, span := observability.StartSpan(ctx, "ReActAgentExecutor.Execute")
	span.SetAttributes(
		attribute.String("agent.mode", string(req.Mode)),
		attribute.String("agent.conversation_id", req.ConversationID),
		attribute.Int("agent.max_steps", e.config.MaxSteps),
		attribute.Int("agent.max_retries", e.config.MaxRetryAttempts),
	)
	defer span.End()

	observability.L().InfoCtx(ctx, "ReActAgentExecutor starting",
		"max_steps", e.config.MaxSteps,
		"max_retries", e.config.MaxRetryAttempts,
		"tools", e.getToolNames(),
	)

	// 初始化对话历史
	messages := []*schema.Message{
		{
			Role:    "system",
			Content: e.buildSystemPrompt(),
		},
		{
			Role:    "user",
			Content: req.UserMessage,
		},
	}

	// 记录思考历史
	thoughtHistory := make([]AgentThought, 0)
	toolCallInfos := make([]ToolCallInfo, 0)

	// ReAct 循环
	for step := 1; step <= e.config.MaxSteps; step++ {
		observability.L().InfoCtx(ctx, "ReAct loop iteration", "step", step)

		// 调用 LLM 获取思考和行动
		llmResponse, err := e.callLLM(ctx, messages)
		if err != nil {
			observability.L().ErrorCtx(ctx, "ReActAgentExecutor LLM call failed", err, "step", step)
			return &ExecuteResponse{
				Error: fmt.Sprintf("LLM调用失败: %v", err),
				Mode:  router.ModeAgent,
			}, err
		}

		// 解析 LLM 响应
		thought, action, actionInput, isFinalAnswer := e.parseLLMResponse(llmResponse)

		// 记录思考
		thoughtHistory = append(thoughtHistory, AgentThought{
			Step:        step,
			Thought:     thought,
			Action:      action,
			ActionInput: actionInput,
		})

		// 如果是最终回答，结束循环
		if isFinalAnswer {
			observability.L().InfoCtx(ctx, "ReAct loop completed with final answer", "step", step)
			return &ExecuteResponse{
				Content:   thought,
				Mode:      router.ModeAgent,
				ToolCalls: toolCallInfos,
			}, nil
		}

		// 如果需要调用工具
		if action != "" {
			// 执行工具调用（带重试）
			toolResult, err := e.executeToolWithRetry(ctx, action, actionInput, step)
			if err != nil {
				observability.L().ErrorCtx(ctx, "ReActAgentExecutor tool execution failed after retries", err,
					"step", step, "tool", action)

				// 添加失败观察到对话历史
				observation := fmt.Sprintf("工具调用失败: %v", err)
				thoughtHistory[len(thoughtHistory)-1].Observation = observation

				messages = append(messages, &schema.Message{
					Role:    "assistant",
					Content: fmt.Sprintf("思考: %s\n行动: %s(%s)\n观察: %s", thought, action, actionInput, observation),
				})

				// 继续循环，让 LLM 决定下一步
				continue
			}

			// 记录工具调用信息
			var args map[string]interface{}
			json.Unmarshal([]byte(actionInput), &args)
			toolCallInfos = append(toolCallInfos, ToolCallInfo{
				ToolName: action,
				Args:     args,
				Result:   toolResult,
			})

			// 将工具执行结果添加到对话历史
			observation := fmt.Sprintf("%v", toolResult)
			thoughtHistory[len(thoughtHistory)-1].Observation = observation

			observability.L().InfoCtx(ctx, "ReActAgentExecutor tool executed",
				"step", step,
				"tool", action,
				"observation_length", len(observation),
			)

			messages = append(messages, &schema.Message{
				Role:    "assistant",
				Content: fmt.Sprintf("思考: %s\n行动: %s(%s)\n观察: %s", thought, action, actionInput, observation),
			})
		}
	}

	// 达到最大步数，返回总结
	observability.L().WarnCtx(ctx, "ReActAgentExecutor reached max steps", "max_steps", e.config.MaxSteps)
	finalSummary := e.buildFinalSummary(thoughtHistory, req.UserMessage)

	return &ExecuteResponse{
		Content:   finalSummary,
		Mode:      router.ModeAgent,
		ToolCalls: toolCallInfos,
	}, nil
}

// buildSystemPrompt 构建系统提示词
func (e *ReActAgentExecutor) buildSystemPrompt() string {
	toolDescriptions := e.getToolDescriptions()

	return `你是一个专业的金融分析助手，具备使用工具的能力。

## 工具列表：
` + toolDescriptions + `

## 思考流程：
1. 分析用户问题，决定是否需要调用工具
2. 如果需要调用工具，请按照格式输出：
   思考: [你的思考过程]
   行动: [工具名称]
   行动输入: [JSON格式的参数]
3. 如果已经获得足够信息可以直接回答用户，请输出：
   思考: [你的总结]
   行动: 总结
   行动输入: 直接回答用户问题

## 注意事项：
- 工具调用的参数必须是有效的JSON格式
- 每次只能调用一个工具
- 如果工具调用失败，请尝试其他方法或总结失败原因
- 不要编造工具结果
- 回答要基于工具返回的信息

请开始分析用户的问题。
`
}

// getToolDescriptions 获取工具描述
func (e *ReActAgentExecutor) getToolDescriptions() string {
	var sb strings.Builder
	for _, tool := range e.tools {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", tool.Name(), tool.Description()))
		sb.WriteString("  参数:\n")
		for _, param := range tool.Parameters() {
			required := "可选"
			if param.Required {
				required = "必填"
			}
			sb.WriteString(fmt.Sprintf("    - %s (%s, %s): %s\n", param.Name, param.Type, required, param.Description))
		}
	}
	return sb.String()
}

// getToolNames 获取工具名称列表
func (e *ReActAgentExecutor) getToolNames() []string {
	names := make([]string, 0, len(e.tools))
	for _, tool := range e.tools {
		names = append(names, tool.Name())
	}
	return names
}

// callLLM 调用 LLM
func (e *ReActAgentExecutor) callLLM(ctx context.Context, messages []*schema.Message) (string, error) {
	content, err := e.llmClient.Generate(ctx, &concurrency.LLMRequest{
		Question: messages[len(messages)-1].Content,
		TaskType: "agent",
		Stream:   false,
		Messages: messages,
	})
	if err != nil {
		return "", err
	}
	return content, nil
}

// parseLLMResponse 解析 LLM 响应
func (e *ReActAgentExecutor) parseLLMResponse(response string) (thought, action, actionInput string, isFinalAnswer bool) {
	// 提取思考
	if idx := strings.Index(response, "思考:"); idx != -1 {
		remaining := response[idx+4:]
		if endIdx := strings.Index(remaining, "\n"); endIdx != -1 {
			thought = strings.TrimSpace(remaining[:endIdx])
		} else {
			thought = strings.TrimSpace(remaining)
		}
	}

	// 提取行动
	if idx := strings.Index(response, "行动:"); idx != -1 {
		remaining := response[idx+3:]
		if endIdx := strings.Index(remaining, "\n"); endIdx != -1 {
			action = strings.TrimSpace(remaining[:endIdx])
		} else {
			action = strings.TrimSpace(remaining)
		}
	}

	// 提取行动输入
	if idx := strings.Index(response, "行动输入:"); idx != -1 {
		actionInput = strings.TrimSpace(response[idx+5:])
	}

	// 判断是否为最终回答
	isFinalAnswer = strings.TrimSpace(action) == "总结" || strings.TrimSpace(action) == "结束" || strings.TrimSpace(action) == "finish"

	return thought, action, actionInput, isFinalAnswer
}

// executeToolWithRetry 执行工具调用（带重试）
func (e *ReActAgentExecutor) executeToolWithRetry(ctx context.Context, toolName, actionInput string, step int) (interface{}, error) {
	// 查找工具
	var tool Tool
	for _, t := range e.tools {
		if t.Name() == toolName {
			tool = t
			break
		}
	}

	if tool == nil {
		return nil, fmt.Errorf("工具不存在: %s", toolName)
	}

	// 解析参数
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(actionInput), &args); err != nil {
		return nil, fmt.Errorf("参数解析失败: %v", err)
	}

	// 执行工具（带重试）
	var lastErr error
	for attempt := 1; attempt <= e.config.MaxRetryAttempts; attempt++ {
		ctx, span := observability.StartSpan(ctx, fmt.Sprintf("Tool.%s", toolName))
		span.SetAttributes(
			attribute.Int("attempt", attempt),
			attribute.Int("step", step),
		)

		result, err := tool.Execute(ctx, args)
		span.End()

		if err == nil {
			observability.L().InfoCtx(ctx, "Tool executed successfully",
				"tool", toolName,
				"attempt", attempt,
				"step", step,
			)
			return result, nil
		}

		lastErr = err
		observability.L().WarnCtx(ctx, "Tool execution failed",
			"tool", toolName,
			"attempt", attempt,
			"step", step,
			"error", err.Error(),
		)

		// 等待后重试
		if attempt < e.config.MaxRetryAttempts {
			time.Sleep(e.config.RetryDelay)
		}
	}

	return nil, fmt.Errorf("工具调用失败（已重试%d次）: %v", e.config.MaxRetryAttempts, lastErr)
}

// buildFinalSummary 构建最终总结
func (e *ReActAgentExecutor) buildFinalSummary(thoughts []AgentThought, originalQuestion string) string {
	var sb strings.Builder
	sb.WriteString("由于思考步数已达上限，以下是当前分析结果：\n\n")
	sb.WriteString("用户问题：" + originalQuestion + "\n\n")
	sb.WriteString("分析过程：\n")

	for _, thought := range thoughts {
		sb.WriteString(fmt.Sprintf("步骤%d: %s\n", thought.Step, thought.Thought))
		if thought.Action != "" && thought.Action != "总结" {
			sb.WriteString(fmt.Sprintf("   行动: %s(%s)\n", thought.Action, thought.ActionInput))
			if thought.Observation != "" {
				obs := thought.Observation
				if len(obs) > 200 {
					obs = obs[:200] + "..."
				}
				sb.WriteString(fmt.Sprintf("   结果: %s\n", obs))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("如需更完整的分析，请重新提问或增加思考步数限制。")
	return sb.String()
}
