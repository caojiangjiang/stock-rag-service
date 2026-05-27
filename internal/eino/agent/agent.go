package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"

	"stock_rag/internal/concurrency"
	"stock_rag/internal/pkgctx"
)

// Deprecated: agent.go 已废弃，建议使用 Coordinator 体系或 Eino 原生 ADK 抽象
// 此文件中的自定义 Agent/Tool/Runner 抽象与 Eino 原生抽象（adk.Agent、tool.BaseTool）重复
//
// 当前项目存在两套 Agent 抽象体系：
// 【自定义抽象】（此文件）
//   - Agent、Tool、Runner、CoordinatorType
// 【Eino 原生抽象】（推荐使用）
//   - adk.Agent、tool.BaseTool、model.ChatModel、compose.Workflow
//
// 建议迁移到：
//   - Coordinator 接口 (coordinator.go)
//   - SupervisorCoordinator / PlanCoordinator / PipelineCoordinator
//   - ToolRegistry (internal/eino/tools/registry.go)
//   - Eino ADK 原生组件

// TaskRuntime 任务运行时上下文（legacy）
type TaskRuntime struct {
	Task            string
	TaskContextInfo string
	MemorySnapshot  []*schema.Message
}

// newLLMRequest 创建 LLMRequest 的辅助函数
func newLLMRequest(taskType string, messages []*schema.Message, metadata map[string]string) *concurrency.LLMRequest {
	return &concurrency.LLMRequest{
		RequestID: fmt.Sprintf("%s-%d", taskType, time.Now().UnixNano()),
		Question:  metadata["task"],
		Messages:  messages,
		TaskType:  taskType,
		Priority:  1,
		Timeout:   2 * time.Minute,
		Stream:    false,
		Metadata:  metadata,
	}
}

// AgentConfig Agent 配置
type AgentConfig struct {
	LLMClient   *concurrency.LLMClient
	Tools       []Tool
	MaxSteps    int
	Temperature float64
}

// Tool 工具接口
type Tool interface {
	Name() string
	Description() string
	Run(ctx context.Context, args map[string]interface{}) (string, error)
}

// Runner 运行器接口
type Runner interface {
	Run(ctx context.Context, task string) (string, error)
}

// Agent Agent 结构体
type Agent struct {
	Config       AgentConfig
	LLMClient    *concurrency.LLMClient
	Tools        map[string]Tool
	Memory       []*schema.Message
	StepTraces   []StepTrace
	ToolFailures map[string]int
	TaskContext  *pkgctx.TaskContext
	Mode         CoordinatorType
	Tracer       Tracer
	mutex        sync.RWMutex
}

// Tracer 追踪接口
type Tracer interface {
	Start(ctx context.Context, spanName, operationName string, tags map[string]interface{}) (context.Context, func(bool, string))
	Record(ctx context.Context, spanName, eventName string, tags map[string]interface{})
}

// AgentFactory Agent 工厂
type AgentFactory struct {
	config    AgentConfig
	llmClient *concurrency.LLMClient
	tools     []Tool
}

// NewAgentFactory 创建 Agent 工厂
func NewAgentFactory(config AgentConfig) *AgentFactory {
	return &AgentFactory{
		config:    config,
		llmClient: config.LLMClient,
		tools:     config.Tools,
	}
}

// CreateAgent 创建 Agent
func (f *AgentFactory) CreateAgent(taskCtx *pkgctx.TaskContext) *Agent {
	agent := NewAgent(f.config)
	agent.SetTaskContext(taskCtx)
	return agent
}

// NewAgent 创建新的 Agent
func NewAgent(config AgentConfig) *Agent {
	tools := make(map[string]Tool)
	for _, tool := range config.Tools {
		tools[tool.Name()] = tool
	}

	return &Agent{
		Config:       config,
		LLMClient:    config.LLMClient,
		Tools:        tools,
		Memory:       make([]*schema.Message, 0),
		StepTraces:   make([]StepTrace, 0),
		ToolFailures: make(map[string]int),
		TaskContext:  pkgctx.NewTaskContext(),
		mutex:        sync.RWMutex{},
	}
}

// SetTaskContext 设置任务上下文
func (a *Agent) SetTaskContext(taskCtx *pkgctx.TaskContext) {
	a.TaskContext = taskCtx
}

// GetTaskContext 获取任务上下文
func (a *Agent) GetTaskContext() *pkgctx.TaskContext {
	return a.TaskContext
}

// getToolsDescription 获取工具描述
func (a *Agent) getToolsDescription() string {
	if len(a.Tools) == 0 {
		return "无可用工具"
	}
	var desc strings.Builder
	for name, tool := range a.Tools {
		desc.WriteString(fmt.Sprintf("- %s: %s\n", name, tool.Description()))
	}
	return desc.String()
}

// executeToolStep 执行工具步骤
func (a *Agent) executeToolStep(ctx context.Context, step int, toolName string, args map[string]interface{}, observationPrefix string) (string, error) {
	tool, exists := a.Tools[toolName]
	if !exists {
		return "", fmt.Errorf("工具 %s 不存在", toolName)
	}

	result, err := tool.Run(ctx, args)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s: %s", observationPrefix, result), nil
}

// Run 执行任务
func (a *Agent) Run(ctx context.Context, task string) (string, error) {
	return a.runWithRetry(ctx, task, 0)
}

func (a *Agent) runWithRetry(ctx context.Context, task string, retryCount int) (string, error) {
	runtime := a.beginTask(task)

	for step := 0; step < a.Config.MaxSteps; step++ {
		result, isFinal, err := a.runStep(ctx, task, step, runtime)
		if err != nil {
			if retryCount < 2 {
				return a.runWithRetry(ctx, task, retryCount+1)
			}
			return "", err
		}

		if isFinal {
			a.completeTask(result)
			return result, nil
		}
	}

	return "", fmt.Errorf("任务执行超过最大步骤限制")
}

func (a *Agent) beginTask(task string) *TaskRuntime {
	taskCtxInfo := ""
	if a.TaskContext != nil {
		data, _ := json.Marshal(a.TaskContext)
		taskCtxInfo = string(data)
	}

	return &TaskRuntime{
		Task:            task,
		TaskContextInfo: taskCtxInfo,
		MemorySnapshot:  append([]*schema.Message{}, a.Memory...),
	}
}

func (a *Agent) completeTask(result string) {
	// 任务完成后的清理工作
}

func (a *Agent) runStep(ctx context.Context, task string, step int, runtime *TaskRuntime) (string, bool, error) {
	// 构建思考消息
	thought := a.buildThought(task, step, runtime)

	// 调用 LLM
	response, err := a.runLLMStage(ctx, "step", task, thought, step, "agent_step", task)
	if err != nil {
		return "", false, err
	}

	// 解析响应
	return a.parseStepResponse(response)
}

func (a *Agent) buildThought(task string, step int, runtime *TaskRuntime) []*schema.Message {
	messages := []*schema.Message{
		{
			Role:    "system",
			Content: a.buildSystemPrompt(),
		},
		{
			Role:    "user",
			Content: task,
		},
	}

	// 添加历史消息
	for _, msg := range a.Memory {
		messages = append(messages, msg)
	}

	return messages
}

func (a *Agent) buildSystemPrompt() string {
	prompt := "你是一个专业的金融分析助手。请根据提供的工具和上下文，帮助用户完成金融分析任务。\n\n"

	if len(a.Tools) > 0 {
		prompt += "可用工具：\n"
		for _, tool := range a.Tools {
			prompt += fmt.Sprintf("%s: %s\n", tool.Name(), tool.Description())
		}
	}

	prompt += "\n请按照以下格式输出：\n"
	prompt += "思考：[你的思考过程]\n"
	prompt += "工具：[工具名称]\n"
	prompt += "参数：[JSON格式的参数]\n"
	prompt += "或者直接给出最终答案。"

	return prompt
}

func (a *Agent) parseStepResponse(response string) (string, bool, error) {
	// 简单的响应解析
	if strings.Contains(response, "最终答案") {
		// 提取最终答案
		start := strings.Index(response, "最终答案") + 4
		return strings.TrimSpace(response[start:]), true, nil
	}

	// 检查是否调用工具
	if strings.Contains(response, "工具：") {
		toolName := ""
		args := ""

		lines := strings.Split(response, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "工具：") {
				toolName = strings.TrimSpace(strings.TrimPrefix(line, "工具："))
			} else if strings.HasPrefix(line, "参数：") {
				args = strings.TrimSpace(strings.TrimPrefix(line, "参数："))
			}
		}

		if toolName != "" && args != "" {
			// 调用工具
			return a.callTool(toolName, args)
		}
	}

	// 默认直接返回响应
	return response, true, nil
}

func (a *Agent) callTool(toolName string, argsJSON string) (string, bool, error) {
	tool, exists := a.Tools[toolName]
	if !exists {
		return "", false, fmt.Errorf("工具 %s 不存在", toolName)
	}

	// 解析参数
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", false, fmt.Errorf("参数解析失败: %v", err)
	}

	// 调用工具
	result, err := tool.Run(context.Background(), args)
	if err != nil {
		return "", false, err
	}

	// 将结果添加到记忆中
	a.mutex.Lock()
	a.Memory = append(a.Memory, &schema.Message{
		Role:    "tool",
		Content: fmt.Sprintf("工具 %s 执行结果: %s", toolName, result),
	})
	a.mutex.Unlock()

	// 继续下一步
	return result, false, nil
}

func (a *Agent) runLLMStage(ctx context.Context, stage, task string, messages []*schema.Message, step int, nodeType, nodeName string) (string, error) {
	metadata := map[string]string{
		"stage":     stage,
		"task":      task,
		"step":      fmt.Sprintf("%d", step),
		"node_type": nodeType,
		"node_name": nodeName,
	}

	req := newLLMRequest(stage, messages, metadata)
	resp, err := a.LLMClient.Generate(ctx, req)
	if err != nil {
		return "", err
	}

	return resp, nil
}

func (a *Agent) appendMemoryMessage(msg *schema.Message) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.Memory = append(a.Memory, msg)
	if len(a.Memory) > 50 {
		a.Memory = a.Memory[len(a.Memory)-50:]
	}
}

func (a *Agent) appendStepTrace(trace StepTrace) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.StepTraces = append(a.StepTraces, trace)
	if len(a.StepTraces) > 100 {
		a.StepTraces = a.StepTraces[len(a.StepTraces)-100:]
	}
}

// NormalizeTaskContext 标准化任务上下文
func (f *AgentFactory) NormalizeTaskContext(userMessage string, params map[string]interface{}) *pkgctx.TaskContext {
	taskCtx := pkgctx.NewTaskContext()
	taskCtx.ConversationSummary = &pkgctx.ConversationSummary{}

	if params != nil {
		if stockCode, ok := params["stock_code"].(string); ok {
			taskCtx.StockCode = stockCode
		}
		if companyName, ok := params["company_name"].(string); ok {
			taskCtx.CompanyName = companyName
		}
		if timeRange, ok := params["time_range"].(string); ok {
			taskCtx.TimeRange = timeRange
		}
	}

	return taskCtx
}
