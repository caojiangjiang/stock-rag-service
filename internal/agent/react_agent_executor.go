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
	"stock_rag/internal/metrics"
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
	MaxRetryAttempts int           // 单工具最大重试次数（仅在不使用 ToolGuard 时生效）
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
	tools     map[string]Tool
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

	toolMap := make(map[string]Tool, len(tools))
	for _, t := range tools {
		toolMap[t.Name()] = t
	}

	return &ReActAgentExecutor{
		name:      "react_agent_executor",
		llmClient: llmClient,
		tools:     toolMap,
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

// ToolObservation 结构化工具观察结果（回写给 LLM）
type ToolObservation struct {
	Step     int         `json:"step"`
	Tool     string      `json:"tool"`
	Status   string      `json:"status"` // success | error | degraded
	Result   interface{} `json:"result,omitempty"`
	Error    string      `json:"error,omitempty"`
	Degraded bool        `json:"degraded,omitempty"`
	Attempt  int         `json:"attempt,omitempty"`
}

// Execute 执行 ReAct 循环
func (e *ReActAgentExecutor) Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error) {
	ctx, span := observability.StartSpan(ctx, "ReActAgentExecutor.Execute")
	mode := resolveExecuteMode(req)
	span.SetAttributes(
		attribute.String("agent.mode", string(mode)),
		attribute.String("agent.conversation_id", req.ConversationID),
		attribute.Int("agent.max_steps", e.config.MaxSteps),
		attribute.Int("agent.max_retries", e.config.MaxRetryAttempts),
	)
	defer span.End()

	observability.L().InfoCtx(ctx, "ReActAgentExecutor starting",
		"max_steps", e.config.MaxSteps,
		"max_retries", e.config.MaxRetryAttempts,
		"tools", e.getToolNames(),
		"route_mode", string(mode),
	)

	messages := []*schema.Message{
		{Role: "system", Content: e.buildSystemPrompt()},
		{Role: "user", Content: e.buildUserPrompt(req)},
	}

	thoughtHistory := make([]AgentThought, 0)
	toolCallInfos := make([]ToolCallInfo, 0)

	for step := 1; step <= e.config.MaxSteps; step++ {
		stepStart := time.Now()
		observability.L().InfoCtx(ctx, "ReAct loop iteration", "step", step)

		llmResponse, err := e.callLLM(ctx, messages)
		if err != nil {
			observability.L().ErrorCtx(ctx, "ReActAgentExecutor LLM call failed", err, "step", step)
			return &ExecuteResponse{
				Error: fmt.Sprintf("LLM调用失败: %v", err),
				Mode:  mode,
			}, err
		}

		thought, action, actionInput, isFinalAnswer := parseLLMResponse(llmResponse)
		record := AgentThought{
			Step:        step,
			Thought:     thought,
			Action:      action,
			ActionInput: actionInput,
		}
		thoughtHistory = append(thoughtHistory, record)

		if isFinalAnswer {
			content := finalizeAnswer(thought, actionInput)
			metrics.RecordAgentStep("react_agent", "final_answer", "success", time.Since(stepStart).Seconds())
			observability.L().InfoCtx(ctx, "ReAct loop completed with final answer", "step", step)
			return &ExecuteResponse{
				Content:   content,
				Mode:      mode,
				ToolCalls: toolCallInfos,
			}, nil
		}

		if action == "" {
			obs := formatObservation(ToolObservation{
				Step:   step,
				Tool:   "",
				Status: "error",
				Error:  "未解析到有效行动，请按格式输出「思考 / 行动 / 行动输入」",
			})
			thoughtHistory[len(thoughtHistory)-1].Observation = obs
			messages = append(messages, e.assistantStepMessage(thought, action, actionInput, obs))
			metrics.RecordAgentStep("react_agent", "parse_action", "error", time.Since(stepStart).Seconds())
			continue
		}

		toolResult, observation, toolInfo, err := e.executeTool(ctx, action, actionInput, step)
		thoughtHistory[len(thoughtHistory)-1].Observation = observation

		if toolInfo != nil {
			toolCallInfos = append(toolCallInfos, *toolInfo)
		}

		messages = append(messages, e.assistantStepMessage(thought, action, actionInput, observation))
		stepStatus := "success"
		if err != nil {
			stepStatus = "error"
		}
		metrics.RecordAgentStep("react_agent", action, stepStatus, time.Since(stepStart).Seconds())

		if err != nil {
			observability.L().WarnCtx(ctx, "ReActAgentExecutor tool execution failed",
				"step", step, "tool", action, "error", err.Error())
			continue
		}

		observability.L().InfoCtx(ctx, "ReActAgentExecutor tool executed",
			"step", step,
			"tool", action,
			"result_type", fmt.Sprintf("%T", toolResult),
		)
	}

	observability.L().WarnCtx(ctx, "ReActAgentExecutor reached max steps", "max_steps", e.config.MaxSteps)
	finalSummary := e.buildFinalSummary(thoughtHistory, req.UserMessage)

	return &ExecuteResponse{
		Content:   finalSummary,
		Mode:      mode,
		ToolCalls: toolCallInfos,
	}, nil
}

func resolveExecuteMode(req *ExecuteRequest) router.RouteMode {
	if req != nil && req.Mode != "" {
		return req.Mode
	}
	return router.ModeAgent
}

func finalizeAnswer(thought, actionInput string) string {
	answer := strings.TrimSpace(actionInput)
	if answer == "" {
		answer = strings.TrimSpace(thought)
	}
	if answer == "" {
		return "抱歉，未能生成有效回答，请重新描述您的问题。"
	}
	return answer
}

func (e *ReActAgentExecutor) buildUserPrompt(req *ExecuteRequest) string {
	var sb strings.Builder
	sb.WriteString(req.UserMessage)

	var ctxParts []string
	if req.StockCode != "" {
		ctxParts = append(ctxParts, "股票代码: "+req.StockCode)
	}
	if req.DocType != "" {
		ctxParts = append(ctxParts, "文档类型: "+req.DocType)
	}
	if req.TimeRange != "" {
		ctxParts = append(ctxParts, "时间范围: "+req.TimeRange)
	}
	if len(ctxParts) > 0 {
		sb.WriteString("\n\n【上下文约束】\n")
		sb.WriteString(strings.Join(ctxParts, "\n"))
	}
	return sb.String()
}

func (e *ReActAgentExecutor) assistantStepMessage(thought, action, actionInput, observation string) *schema.Message {
	return &schema.Message{
		Role: "assistant",
		Content: fmt.Sprintf("思考: %s\n行动: %s\n行动输入: %s\n观察: %s",
			thought, action, actionInput, observation),
	}
}

// buildSystemPrompt 构建系统提示词
func (e *ReActAgentExecutor) buildSystemPrompt() string {
	toolDescriptions := e.getToolDescriptions()

	return `你是一个专业的金融分析助手，具备使用工具的能力。

## 工具列表：
` + toolDescriptions + `

## 思考流程（ReAct）：
1. 分析用户问题，决定是否需要调用工具
2. 若需调用工具，严格按以下格式输出（每次仅一个工具）：
   思考: [你的推理]
   行动: [工具名称，必须与工具列表完全一致]
   行动输入: [单行 JSON 参数]
3. 若信息已足够，输出最终回答：
   思考: [简要总结]
   行动: 总结
   行动输入: [给用户的完整回答]

## 注意事项：
- 行动输入必须是合法 JSON，且只占一行
- 每次只能调用一个工具
- 观察区由系统回填，你不要编造工具结果
- 工具失败时请换用其他工具或基于已有信息总结
- 回答须基于工具返回的信息，并标注数据来源不确定性

请开始分析用户的问题。
`
}

func (e *ReActAgentExecutor) getToolDescriptions() string {
	var sb strings.Builder
	for _, name := range e.getToolNames() {
		tool := e.tools[name]
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

func (e *ReActAgentExecutor) getToolNames() []string {
	names := make([]string, 0, len(e.tools))
	for name := range e.tools {
		names = append(names, name)
	}
	return names
}

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

// parseLLMResponse 解析 LLM 响应中的思考、行动与行动输入
func parseLLMResponse(response string) (thought, action, actionInput string, isFinalAnswer bool) {
	lines := strings.Split(response, "\n")
	var inputLines []string
	collectingInput := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "思考:"):
			thought = strings.TrimSpace(strings.TrimPrefix(trimmed, "思考:"))
			collectingInput = false
		case strings.HasPrefix(trimmed, "行动:"):
			action = strings.TrimSpace(strings.TrimPrefix(trimmed, "行动:"))
			collectingInput = false
		case strings.HasPrefix(trimmed, "行动输入:"):
			actionInput = strings.TrimSpace(strings.TrimPrefix(trimmed, "行动输入:"))
			inputLines = nil
			if actionInput != "" {
				inputLines = append(inputLines, actionInput)
			}
			collectingInput = true
		case collectingInput && trimmed != "":
			inputLines = append(inputLines, trimmed)
		}
	}

	if len(inputLines) > 0 {
		actionInput = strings.Join(inputLines, " ")
	}

	action = strings.TrimSpace(action)
	isFinalAnswer = isFinalAction(action)
	return thought, action, actionInput, isFinalAnswer
}

func isFinalAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "总结", "结束", "finish", "final", "answer", "final_answer":
		return true
	default:
		return false
	}
}

func (e *ReActAgentExecutor) executeTool(
	ctx context.Context,
	toolName, actionInput string,
	step int,
) (interface{}, string, *ToolCallInfo, error) {
	tool, ok := e.tools[toolName]
	if !ok {
		obs := formatObservation(ToolObservation{
			Step:   step,
			Tool:   toolName,
			Status: "error",
			Error:  fmt.Sprintf("工具不存在: %s，可用工具: %s", toolName, strings.Join(e.getToolNames(), ", ")),
		})
		return nil, obs, nil, fmt.Errorf("工具不存在: %s", toolName)
	}

	args, err := parseActionInput(actionInput)
	if err != nil {
		obs := formatObservation(ToolObservation{
			Step:   step,
			Tool:   toolName,
			Status: "error",
			Error:  err.Error(),
		})
		return nil, obs, nil, err
	}

	var lastErr error
	var result interface{}
	status := "success"
	degraded := false

	for attempt := 1; attempt <= e.config.MaxRetryAttempts; attempt++ {
		ctx, span := observability.StartSpan(ctx, fmt.Sprintf("Tool.%s", toolName))
		span.SetAttributes(attribute.Int("attempt", attempt), attribute.Int("step", step))

		result, lastErr = tool.Execute(ctx, args)
		span.End()

		if lastErr == nil {
			if body, ok := result.(string); ok && strings.Contains(body, `"error":"tool_degraded"`) {
				degraded = true
				status = "degraded"
			}
			obs := formatObservation(ToolObservation{
				Step:     step,
				Tool:     toolName,
				Status:   status,
				Result:   result,
				Degraded: degraded,
				Attempt:  attempt,
			})
			info := &ToolCallInfo{ToolName: toolName, Args: args, Result: result}
			return result, obs, info, nil
		}

		if attempt < e.config.MaxRetryAttempts {
			time.Sleep(e.config.RetryDelay)
		}
	}

	obs := formatObservation(ToolObservation{
		Step:    step,
		Tool:    toolName,
		Status:  "error",
		Error:   lastErr.Error(),
		Attempt: e.config.MaxRetryAttempts,
	})
	info := &ToolCallInfo{ToolName: toolName, Args: args, Result: map[string]string{"error": lastErr.Error()}}
	return nil, obs, info, lastErr
}

func parseActionInput(actionInput string) (map[string]interface{}, error) {
	actionInput = strings.TrimSpace(actionInput)
	if actionInput == "" {
		return map[string]interface{}{}, nil
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(actionInput), &args); err != nil {
		return nil, fmt.Errorf("参数解析失败: %v", err)
	}
	return args, nil
}

func formatObservation(obs ToolObservation) string {
	data, err := json.Marshal(obs)
	if err != nil {
		return fmt.Sprintf(`{"step":%d,"tool":"%s","status":"error","error":"marshal failed"}`, obs.Step, obs.Tool)
	}
	return string(data)
}

func (e *ReActAgentExecutor) buildFinalSummary(thoughts []AgentThought, originalQuestion string) string {
	var sb strings.Builder
	sb.WriteString("由于思考步数已达上限，以下是当前分析结果：\n\n")
	sb.WriteString("用户问题：" + originalQuestion + "\n\n")
	sb.WriteString("分析过程：\n")

	for _, thought := range thoughts {
		sb.WriteString(fmt.Sprintf("步骤%d: %s\n", thought.Step, thought.Thought))
		if thought.Action != "" && !isFinalAction(thought.Action) {
			sb.WriteString(fmt.Sprintf("   行动: %s(%s)\n", thought.Action, thought.ActionInput))
			if thought.Observation != "" {
				obs := thought.Observation
				if len(obs) > 300 {
					obs = obs[:300] + "..."
				}
				sb.WriteString(fmt.Sprintf("   观察: %s\n", obs))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("如需更完整的分析，请重新提问或增加思考步数限制。")
	return sb.String()
}
