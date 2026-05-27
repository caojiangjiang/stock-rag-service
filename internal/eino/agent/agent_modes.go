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

// Deprecated: agent_modes.go 已废弃，建议使用 Coordinator 体系或 Eino 原生 ADK 抽象
// 此文件已统一到 Coordinator 体系，以下为兼容性别名
//
// 建议迁移到：
//   - Coordinator 接口 (coordinator.go)
//   - CoordinatorType 类型定义
//   - CoordinatorFactory 创建协调器

// 兼容性别名：将旧的 AgentMode 映射到 CoordinatorType
const (
	// ModeReAct ReAct 模式（legacy）- 映射到 Supervisor
	ModeReAct CoordinatorType = CoordinatorTypeSupervisor
	// ModePlanAndExecute Plan-and-Execute 模式（legacy）- 映射到 Plan
	ModePlanAndExecute CoordinatorType = CoordinatorTypePlan
	// ModeMultiAgent 多 Agent 模式（legacy）- 映射到 Committee
	ModeMultiAgent CoordinatorType = CoordinatorTypeCommittee
	// ModeEinoMultiAgent 使用 Eino ADK 的多 Agent 模式（legacy）- 映射到 Supervisor
	ModeEinoMultiAgent CoordinatorType = CoordinatorTypeSupervisor
	// ModePeerAgent 对等模式（legacy）- 映射到 Peer
	ModePeerAgent CoordinatorType = CoordinatorTypePeer
	// ModeHybridAgent 混合模式（legacy）- 映射到 Deep
	ModeHybridAgent CoordinatorType = CoordinatorTypeDeep
)

// AgentModeConfig 模式配置（legacy）
type AgentModeConfig struct {
	Mode        CoordinatorType
	MaxSteps    int
	Temperature float64
}

// ReactAgent ReAct 模式 Agent
type ReactAgent struct {
	*Agent
	LLMClient *concurrency.LLMClient
}

// NewReactAgent 创建 ReAct Agent
func NewReactAgent(config AgentConfig) *ReactAgent {
	baseAgent := NewAgent(config)
	return &ReactAgent{
		Agent:     baseAgent,
		LLMClient: baseAgent.LLMClient,
	}
}

// Run 执行 ReAct 模式
func (a *ReactAgent) Run(ctx context.Context, task string) (string, error) {
	return a.Agent.Run(ctx, task)
}

// PlanExecuteAgent Plan-and-Execute 模式 Agent
type PlanExecuteAgent struct {
	*Agent
	LLMClient *concurrency.LLMClient
}

// NewPlanExecuteAgent 创建 Plan-and-Execute Agent
func NewPlanExecuteAgent(config AgentConfig) *PlanExecuteAgent {
	baseAgent := NewAgent(config)
	return &PlanExecuteAgent{
		Agent:     baseAgent,
		LLMClient: baseAgent.LLMClient,
	}
}

func NewPlanExecuteAgentWithBase(base *Agent) *PlanExecuteAgent {
	planAgent := NewPlanExecuteAgent(base.Config)
	planAgent.AttachBaseAgent(base)
	return planAgent
}

func (a *PlanExecuteAgent) AttachBaseAgent(base *Agent) {
	a.Agent = base
	a.LLMClient = base.LLMClient
}

// Run 执行 Plan-and-Execute 模式
func (a *PlanExecuteAgent) Run(ctx context.Context, task string) (string, error) {
	runtime := a.beginTask(task)

	// 1. 生成计划
	plan, err := a.generatePlan(ctx, task, runtime.TaskContextInfo)
	if err != nil {
		return "", err
	}

	// 2. 解析计划
	steps, err := a.parsePlan(plan)
	if err != nil {
		return "", err
	}

	// 3. 执行计划
	results, err := a.executePlan(ctx, steps)
	if err != nil {
		return "", err
	}

	// 4. 总结结果
	summary, err := a.summarizeResults(ctx, task, runtime.TaskContextInfo, len(steps)+1, steps, results)
	if err != nil {
		return "", err
	}

	a.completeTask(summary)

	return summary, nil
}

// generatePlan 生成执行计划
func (a *PlanExecuteAgent) generatePlan(ctx context.Context, task string, sessionStateInfo string) (string, error) {
	// 构建计划提示
	planPrompt := fmt.Sprintf(`
你是一个股票分析专家。请为以下任务制定详细的执行计划：

任务：%s

	%s

可用工具：
%s

请输出详细的执行步骤，每个步骤包含：
1. 步骤描述
2. 所需工具
3. 工具参数

格式：
步骤 1: [描述]
工具: [工具名称]
参数: [JSON 格式参数]

步骤 2: [描述]
工具: [工具名称]
参数: [JSON 格式参数]

...
	`, task, sessionStateInfo, a.getToolsDescription())

	// 生成计划
	planMessages := []*schema.Message{
		{
			Role:    "system",
			Content: "你是一个任务规划专家，擅长为复杂任务制定详细的执行计划。",
		},
		{
			Role:    "user",
			Content: planPrompt,
		},
	}

	return a.runLLMStage(ctx, "plan", task, planMessages, 0, "plan_generation", task)
}

// PlanStep 计划步骤
type PlanStep struct {
	Description string                 `json:"description"`
	ToolName    string                 `json:"tool_name"`
	Arguments   map[string]interface{} `json:"arguments"`
}

// parsePlan 解析计划
func (a *PlanExecuteAgent) parsePlan(plan string) ([]PlanStep, error) {
	// 简单的计划解析实现
	lines := strings.Split(plan, "\n")
	var steps []PlanStep
	var currentStep PlanStep
	var inStep bool

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "步骤 ") && strings.Contains(line, ":") {
			// 保存当前步骤
			if inStep && currentStep.ToolName != "" {
				steps = append(steps, currentStep)
			}
			// 开始新步骤
			currentStep = PlanStep{}
			currentStep.Description = strings.TrimSpace(strings.TrimPrefix(line, "步骤 1: "))
			if strings.HasPrefix(line, "步骤 1:") {
				currentStep.Description = strings.TrimSpace(strings.TrimPrefix(line, "步骤 1:"))
			} else if strings.HasPrefix(line, "步骤 2:") {
				currentStep.Description = strings.TrimSpace(strings.TrimPrefix(line, "步骤 2:"))
			} else if strings.HasPrefix(line, "步骤 3:") {
				currentStep.Description = strings.TrimSpace(strings.TrimPrefix(line, "步骤 3:"))
			}
			inStep = true
		} else if strings.HasPrefix(line, "工具:") {
			currentStep.ToolName = strings.TrimSpace(strings.TrimPrefix(line, "工具:"))
		} else if strings.HasPrefix(line, "参数:") {
			argsStr := strings.TrimSpace(strings.TrimPrefix(line, "参数:"))
			args := make(map[string]interface{})
			if argsStr != "" {
				if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
					// 解析失败时使用空参数
					args = make(map[string]interface{})
				}
			}
			currentStep.Arguments = args
		}
	}

	// 保存最后一个步骤
	if inStep && currentStep.ToolName != "" {
		steps = append(steps, currentStep)
	}

	return steps, nil
}

// executePlan 执行计划
func (a *PlanExecuteAgent) executePlan(ctx context.Context, steps []PlanStep) ([]string, error) {
	var results []string

	for i, step := range steps {
		observation, _ := a.executeToolStep(ctx, i+1, step.ToolName, step.Arguments, fmt.Sprintf("步骤 %d 工具 %s 执行结果", i+1, step.ToolName))
		results = append(results, fmt.Sprintf("步骤 %d 执行结果: %s", i+1, observation))
	}

	return results, nil
}

// summarizeResults 总结执行结果
func (a *PlanExecuteAgent) summarizeResults(ctx context.Context, task string, sessionStateInfo string, traceStep int, steps []PlanStep, results []string) (string, error) {
	// 构建总结提示
	summaryPrompt := fmt.Sprintf(`
请总结以下任务的执行结果：

任务：%s

	%s

执行计划：
	`, task, sessionStateInfo)

	for i, step := range steps {
		summaryPrompt += fmt.Sprintf("步骤 %d: %s\n工具: %s\n参数: %v\n执行结果: %s\n\n",
			i+1, step.Description, step.ToolName, step.Arguments, results[i])
	}

	summaryPrompt += "请提供详细的总结报告，包括：\n1. 任务完成情况\n2. 执行过程中的关键发现\n3. 最终结论"

	// 生成总结
	summaryMessages := []*schema.Message{
		{
			Role:    "system",
			Content: "你是一个总结专家，擅长总结任务执行结果。",
		},
		{
			Role:    "user",
			Content: summaryPrompt,
		},
	}

	return a.runLLMStage(ctx, "summary", task, summaryMessages, traceStep, "plan_summary", task)
}

// MultiAgent 多 Agent 模式
type MultiAgent struct {
	*Agent
	LLMClient   *concurrency.LLMClient
	specialists map[string]*Agent
}

// NewMultiAgent 创建多 Agent
func NewMultiAgent(config AgentConfig) *MultiAgent {
	baseAgent := NewAgent(config)

	// 创建专家 Agent
	specialists := make(map[string]*Agent)

	// 数据收集专家
	dataCollectorConfig := config
	dataCollectorConfig.Tools = []Tool{}
	for _, tool := range config.Tools {
		if tool.Name() == "collect_stock_data" {
			dataCollectorConfig.Tools = append(dataCollectorConfig.Tools, tool)
		}
	}
	specialists["data_collector"] = NewAgent(dataCollectorConfig)

	// 财务分析专家
	financialAnalyzerConfig := config
	financialAnalyzerConfig.Tools = []Tool{}
	for _, tool := range config.Tools {
		if tool.Name() == "analyze_financial" {
			financialAnalyzerConfig.Tools = append(financialAnalyzerConfig.Tools, tool)
		}
	}
	specialists["financial_analyzer"] = NewAgent(financialAnalyzerConfig)

	// 行业分析专家
	industryAnalyzerConfig := config
	industryAnalyzerConfig.Tools = []Tool{}
	for _, tool := range config.Tools {
		if tool.Name() == "search_reports" {
			industryAnalyzerConfig.Tools = append(industryAnalyzerConfig.Tools, tool)
		}
	}
	specialists["industry_analyzer"] = NewAgent(industryAnalyzerConfig)

	// 风险评估专家
	riskAnalyzerConfig := config
	riskAnalyzerConfig.Tools = []Tool{}
	for _, tool := range config.Tools {
		if tool.Name() == "compare_years" {
			riskAnalyzerConfig.Tools = append(riskAnalyzerConfig.Tools, tool)
		}
	}
	specialists["risk_analyzer"] = NewAgent(riskAnalyzerConfig)

	return &MultiAgent{
		Agent:       baseAgent,
		LLMClient:   baseAgent.LLMClient,
		specialists: specialists,
	}
}

func NewMultiAgentWithBase(base *Agent) *MultiAgent {
	multiAgent := NewMultiAgent(base.Config)
	multiAgent.AttachBaseAgent(base)
	return multiAgent
}

func (a *MultiAgent) AttachBaseAgent(base *Agent) {
	a.Agent = base
	a.LLMClient = base.LLMClient
	for _, specialist := range a.specialists {
		specialist.LLMClient = base.LLMClient
	}
}

// Run 执行多 Agent 模式
func (a *MultiAgent) Run(ctx context.Context, task string) (string, error) {
	runtime := a.beginTask(task)

	// 1. 分析任务需求，确定需要的专家
	experts, subtasks, err := a.analyzeTask(ctx, task, runtime.TaskContextInfo)
	if err != nil {
		return "", err
	}

	// 2. 协调专家执行子任务（并行执行）
	expertResults, err := a.coordinateExperts(ctx, experts, subtasks)
	if err != nil {
		return "", err
	}

	// 3. 整合专家结果
	summary, err := a.integrateResults(ctx, task, runtime.TaskContextInfo, len(experts)+1, expertResults)
	if err != nil {
		return "", err
	}

	a.completeTask(summary)

	return summary, nil
}

// analyzeTask 分析任务需求
func (a *MultiAgent) analyzeTask(ctx context.Context, task string, sessionStateInfo string) ([]string, map[string]string, error) {
	// 构建任务分析提示
	analyzePrompt := fmt.Sprintf(`
分析以下任务，确定需要哪些专家和子任务：

任务：%s

	%s

可用专家：
1. data_collector：负责收集股票的基本信息、财务数据和新闻
2. financial_analyzer：负责分析股票的财务状况和指标
3. industry_analyzer：负责分析行业趋势和竞争格局
4. risk_analyzer：负责评估投资风险和对比分析

请输出：
1. 需要的专家列表（逗号分隔）
2. 每个专家的子任务（JSON格式）

格式：
专家列表：data_collector,financial_analyzer
子任务：{"data_collector": "收集茅台的财务数据", "financial_analyzer": "分析茅台的财务状况"}
	`, task, sessionStateInfo)

	// 分析任务
	analyzeMessages := []*schema.Message{
		{
			Role:    "system",
			Content: "你是一个任务分析专家，擅长分析任务需求并确定所需的专业领域。",
		},
		{
			Role:    "user",
			Content: analyzePrompt,
		},
	}
	response, err := a.runLLMStage(ctx, "analyze", task, analyzeMessages, 0, "expert_routing", task)
	if err != nil {
		return nil, nil, err
	}

	// 解析结果
	var experts []string
	subtasks := make(map[string]string)

	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "专家列表：") {
			expertsStr := strings.TrimSpace(strings.TrimPrefix(line, "专家列表："))
			experts = strings.Split(expertsStr, ",")
		} else if strings.HasPrefix(line, "子任务：") {
			subtasksStr := strings.TrimSpace(strings.TrimPrefix(line, "子任务："))
			if err := json.Unmarshal([]byte(subtasksStr), &subtasks); err != nil {
				// 解析失败时使用默认值
				subtasks = make(map[string]string)
			}
		}
	}

	return experts, subtasks, nil
}

// coordinateExperts 协调专家执行子任务（并行）
func (a *MultiAgent) coordinateExperts(ctx context.Context, experts []string, subtasks map[string]string) (map[string]string, error) {
	results := make(map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error

	for i, expertName := range experts {
		wg.Add(1)
		go func(idx int, name string) {
			defer wg.Done()
			stepStart := time.Now()
			expert, exists := a.specialists[name]
			if !exists {
				observation := fmt.Sprintf("专家 %s 不存在", name)
				mu.Lock()
				a.appendStepTrace(StepTrace{
					StepID:    fmt.Sprintf("%d", idx+1),
					ToolName:  fmt.Sprintf("expert:%s", name),
					Input:     map[string]interface{}{"task": subtasks[name]},
					Output:    observation,
					EndTime:   time.Now(),
					Status:    TaskStatusFailed,
					StartTime: stepStart,
				})
				results[name] = observation
				mu.Unlock()
				return
			}

			// 获取子任务
			subtask, ok := subtasks[name]
			if !ok {
				subtask = "执行相关任务"
			}

			// 创建专家上下文
			expertCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			// 为专家创建独立的状态副本
			expertCopy := &Agent{
				Config:       expert.Config,
				LLMClient:    expert.LLMClient,
				Tools:        expert.Tools,
				Memory:       append([]*schema.Message{}, a.Memory...),
				StepTraces:   append([]StepTrace{}, a.StepTraces...),
				ToolFailures: make(map[string]int),
				TaskContext:  a.TaskContext,
				Mode:         expert.Mode,
				Tracer:       expert.Tracer,
			}

			// 执行子任务
			result, err := expertCopy.Run(expertCtx, subtask)
			// 同步状态回主Agent
			a.syncParentRuntimeState(expertCopy)
			if err != nil {
				result = fmt.Sprintf("执行失败: %v", err)
				if firstErr == nil {
					firstErr = err
				}
			}

			mu.Lock()
			a.appendMemoryMessage(&schema.Message{
				Role:    "tool",
				Content: fmt.Sprintf("专家 %s 执行结果: %s", name, result),
			})
			a.appendStepTrace(StepTrace{
				StepID:    fmt.Sprintf("%d", idx+1),
				ToolName:  fmt.Sprintf("expert:%s", name),
				Input:     map[string]interface{}{"task": subtask},
				Output:    result,
				EndTime:   time.Now(),
				Status:    TaskStatusCompleted,
				StartTime: stepStart,
			})
			results[name] = result
			mu.Unlock()
		}(i, expertName)
	}
	wg.Wait()
	return results, firstErr
}

// integrateResults 整合专家结果
func (a *MultiAgent) integrateResults(ctx context.Context, task string, sessionStateInfo string, traceStep int, expertResults map[string]string) (string, error) {
	// 构建整合提示
	integratePrompt := fmt.Sprintf(`
请整合以下专家的执行结果，提供最终答案：

任务：%s

	%s

专家执行结果：
	`, task, sessionStateInfo)

	for expert, result := range expertResults {
		integratePrompt += fmt.Sprintf("%s: %s\n\n", expert, result)
	}

	integratePrompt += "请提供详细的整合报告，包括：\n1. 任务完成情况\n2. 各专家的主要发现\n3. 最终结论\n4. 专家意见的一致性分析"

	// 生成整合报告
	integrateMessages := []*schema.Message{
		{
			Role:    "system",
			Content: "你是一个结果整合专家，擅长整合多个专家的执行结果，分析专家意见的一致性，并提供全面的最终报告。",
		},
		{
			Role:    "user",
			Content: integratePrompt,
		},
	}

	return a.runLLMStage(ctx, "integrate", task, integrateMessages, traceStep, "expert_integration", task)
}

// parseSpecialistCall 解析专家调用
func (a *MultiAgent) parseSpecialistCall(response string) (string, string, error) {
	// 简单实现：提取专家名称和子任务
	if strings.Contains(response, "data_collector") {
		return "data_collector", "收集股票数据", nil
	} else if strings.Contains(response, "financial_analyzer") {
		return "financial_analyzer", "分析股票财务状况", nil
	} else if strings.Contains(response, "industry_analyzer") {
		return "industry_analyzer", "分析行业趋势", nil
	} else if strings.Contains(response, "risk_analyzer") {
		return "risk_analyzer", "评估投资风险", nil
	}
	return "", "", fmt.Errorf("未找到专家调用")
}

func (a *MultiAgent) syncSpecialistRuntime(expert *Agent) {
	expert.LLMClient = a.LLMClient
	expert.TaskContext = a.TaskContext
	expert.Memory = a.Memory
	expert.StepTraces = a.StepTraces
	expert.ToolFailures = a.ToolFailures
}

func (a *MultiAgent) syncParentRuntimeState(expert *Agent) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if len(expert.Memory) > len(a.Memory) {
		a.Memory = append(a.Memory, expert.Memory[len(a.Memory):]...)
		if len(a.Memory) > 50 {
			a.Memory = a.Memory[len(a.Memory)-50:]
		}
	}

	if len(expert.StepTraces) > len(a.StepTraces) {
		a.StepTraces = append(a.StepTraces, expert.StepTraces[len(a.StepTraces):]...)
		if len(a.StepTraces) > 100 {
			a.StepTraces = a.StepTraces[len(a.StepTraces)-100:]
		}
	}

	for toolName, count := range expert.ToolFailures {
		a.ToolFailures[toolName] += count
	}
}

// PeerAgent 对等模式 Agent
type PeerAgent struct {
	*Agent
	peers map[string]*Agent
}

// NewPeerAgent 创建对等模式 Agent
func NewPeerAgent(config AgentConfig) *PeerAgent {
	baseAgent := NewAgent(config)

	// 创建对等 Agent
	peers := make(map[string]*Agent)

	// 数据收集 Agent
	dataCollectorConfig := config
	dataCollectorConfig.Tools = []Tool{}
	for _, tool := range config.Tools {
		if tool.Name() == "collect_stock_data" {
			dataCollectorConfig.Tools = append(dataCollectorConfig.Tools, tool)
		}
	}
	peers["data_collector"] = NewAgent(dataCollectorConfig)

	// 财务分析 Agent
	financialAnalyzerConfig := config
	financialAnalyzerConfig.Tools = []Tool{}
	for _, tool := range config.Tools {
		if tool.Name() == "analyze_financial" {
			financialAnalyzerConfig.Tools = append(financialAnalyzerConfig.Tools, tool)
		}
	}
	peers["financial_analyzer"] = NewAgent(financialAnalyzerConfig)

	// 行业分析 Agent
	industryAnalyzerConfig := config
	industryAnalyzerConfig.Tools = []Tool{}
	for _, tool := range config.Tools {
		if tool.Name() == "search_reports" {
			industryAnalyzerConfig.Tools = append(industryAnalyzerConfig.Tools, tool)
		}
	}
	peers["industry_analyzer"] = NewAgent(industryAnalyzerConfig)

	// 风险评估 Agent
	riskAnalyzerConfig := config
	riskAnalyzerConfig.Tools = []Tool{}
	for _, tool := range config.Tools {
		if tool.Name() == "compare_years" {
			riskAnalyzerConfig.Tools = append(riskAnalyzerConfig.Tools, tool)
		}
	}
	peers["risk_analyzer"] = NewAgent(riskAnalyzerConfig)

	return &PeerAgent{
		Agent: baseAgent,
		peers: peers,
	}
}

// Run 执行对等模式
func (a *PeerAgent) Run(ctx context.Context, task string) (string, error) {
	runtime := a.beginTask(task)

	// 1. 与其他Agent协商任务分配
	assignments, err := a.negotiateTask(ctx, task, runtime.TaskContextInfo)
	if err != nil {
		return "", err
	}

	// 2. 执行自己的子任务（带重试）
	var result string
	err = RetryWithBackoff(3, 100*time.Millisecond, func() error {
		var innerErr error
		result, innerErr = a.executeOwnTask(ctx, assignments["coordinator"])
		return innerErr
	})
	if err != nil {
		return "", err
	}

	// 3. 与其他Agent交换结果
	results, err := a.exchangeResults(ctx, assignments, result)
	if err != nil {
		return "", err
	}

	// 4. 整合结果
	summary, err := a.integrateResults(ctx, task, runtime.TaskContextInfo, len(assignments)+1, results)
	if err != nil {
		return "", err
	}

	a.completeTask(summary)

	return summary, nil
}

// syncPeerRuntime 同步对等Agent运行时状态
func (a *PeerAgent) syncPeerRuntime(peer *Agent) {
	peer.LLMClient = a.LLMClient
	peer.TaskContext = a.TaskContext
	peer.Memory = a.Memory
	peer.StepTraces = a.StepTraces
	peer.ToolFailures = a.ToolFailures
}

// integrateResults 整合对等Agent结果
func (a *PeerAgent) integrateResults(ctx context.Context, task string, sessionStateInfo string, traceStep int, peerResults map[string]string) (string, error) {
	// 构建整合提示
	integratePrompt := fmt.Sprintf(`
请整合以下对等Agent的执行结果，提供最终答案：

任务：%s

	%s

对等Agent执行结果：
	`, task, sessionStateInfo)

	for peer, result := range peerResults {
		integratePrompt += fmt.Sprintf("%s: %s\n\n", peer, result)
	}

	integratePrompt += "请提供详细的整合报告，包括：\n1. 任务完成情况\n2. 各Agent的主要发现\n3. 最终结论\n4. Agent意见的一致性分析"

	// 生成整合报告
	integrateMessages := []*schema.Message{
		{
			Role:    "system",
			Content: "你是一个结果整合专家，擅长整合多个对等Agent的执行结果，分析Agent意见的一致性，并提供全面的最终报告。",
		},
		{
			Role:    "user",
			Content: integratePrompt,
		},
	}

	return a.runLLMStage(ctx, "integrate", task, integrateMessages, traceStep, "peer_integration", task)
}

// HybridAgent 混合模式 Agent
type HybridAgent struct {
	*MultiAgent
	peers map[string]*MultiAgent
}

// NewHybridAgent 创建混合模式 Agent
func NewHybridAgent(config AgentConfig) *HybridAgent {
	baseAgent := NewMultiAgent(config)

	// 创建对等 MultiAgent
	peers := make(map[string]*MultiAgent)

	// 股票分析 MultiAgent
	stockAnalyzerConfig := config
	stockAnalyzerConfig.Tools = []Tool{}
	for _, tool := range config.Tools {
		if tool.Name() == "collect_stock_data" || tool.Name() == "analyze_financial" {
			stockAnalyzerConfig.Tools = append(stockAnalyzerConfig.Tools, tool)
		}
	}
	peers["stock_analyzer"] = NewMultiAgent(stockAnalyzerConfig)

	// 行业分析 MultiAgent
	industryAnalyzerConfig := config
	industryAnalyzerConfig.Tools = []Tool{}
	for _, tool := range config.Tools {
		if tool.Name() == "search_reports" || tool.Name() == "compare_years" {
			industryAnalyzerConfig.Tools = append(industryAnalyzerConfig.Tools, tool)
		}
	}
	peers["industry_analyzer"] = NewMultiAgent(industryAnalyzerConfig)

	// 风险评估 MultiAgent
	riskAnalyzerConfig := config
	riskAnalyzerConfig.Tools = []Tool{}
	for _, tool := range config.Tools {
		if tool.Name() == "compare_years" || tool.Name() == "analyze_financial" {
			riskAnalyzerConfig.Tools = append(riskAnalyzerConfig.Tools, tool)
		}
	}
	peers["risk_analyzer"] = NewMultiAgent(riskAnalyzerConfig)

	return &HybridAgent{
		MultiAgent: baseAgent,
		peers:      peers,
	}
}

// Run 执行混合模式
func (a *HybridAgent) Run(ctx context.Context, task string) (string, error) {
	runtime := a.beginTask(task)

	// 1. 分析任务需求，确定需要的模式
	mode, assignments, err := a.analyzeTaskMode(ctx, task, runtime.TaskContextInfo)
	if err != nil {
		return "", err
	}

	// 2. 根据模式执行任务
	var result string
	if mode == "multi_agent" {
		// 使用主从模式执行
		result, err = a.MultiAgent.Run(ctx, task)
	} else if mode == "peer_agent" {
		// 使用对等模式执行
		result, err = a.executePeerMode(ctx, assignments)
	} else {
		// 默认使用主从模式
		result, err = a.MultiAgent.Run(ctx, task)
	}

	if err != nil {
		return "", err
	}

	a.completeTask(result)

	return result, nil
}

// analyzeTaskMode 分析任务模式
func (a *HybridAgent) analyzeTaskMode(ctx context.Context, task string, sessionStateInfo string) (string, map[string]string, error) {
	// 构建模式分析提示
	modePrompt := fmt.Sprintf(`
分析以下任务，确定应该使用主从模式还是对等模式：

任务：%s

	%s

可用模式：
1. multi_agent：主从模式，适合任务明确、需要集中控制的场景
2. peer_agent：对等模式，适合任务复杂、需要多Agent协作的场景

请输出：
1. 模式选择（multi_agent 或 peer_agent）
2. 任务分配（JSON格式）

格式：
模式：multi_agent
任务分配：{"data_collector": "收集茅台的财务数据", "financial_analyzer": "分析茅台的财务状况"}
	`, task, sessionStateInfo)

	// 分析任务模式
	modeMessages := []*schema.Message{
		{
			Role:    "system",
			Content: "你是一个模式分析专家，擅长根据任务需求选择合适的多Agent模式。",
		},
		{
			Role:    "user",
			Content: modePrompt,
		},
	}
	response, err := a.runLLMStage(ctx, "analyze_mode", task, modeMessages, 0, "hybrid_analysis", task)
	if err != nil {
		return "", nil, err
	}

	// 解析结果
	lines := strings.Split(response, "\n")
	mode := "multi_agent"
	assignments := make(map[string]string)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "模式：") {
			mode = strings.TrimPrefix(line, "模式：")
		} else if strings.HasPrefix(line, "任务分配：") {
			jsonStr := strings.TrimPrefix(line, "任务分配：")
			err := json.Unmarshal([]byte(jsonStr), &assignments)
			if err != nil {
				return "", nil, err
			}
		}
	}

	return mode, assignments, nil
}

// executePeerMode 执行对等模式
func (a *HybridAgent) executePeerMode(ctx context.Context, assignments map[string]string) (string, error) {
	results := make(map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error

	for peerName, subtask := range assignments {
		wg.Add(1)
		go func(name string, task string) {
			defer wg.Done()
			peer, exists := a.peers[name]
			if !exists {
				result := fmt.Sprintf("MultiAgent %s 不存在", name)
				mu.Lock()
				results[name] = result
				mu.Unlock()
				return
			}

			// 执行子任务
			result, err := peer.Run(ctx, task)
			if err != nil {
				result = fmt.Sprintf("执行失败: %v", err)
				if firstErr == nil {
					firstErr = err
				}
			}

			mu.Lock()
			results[name] = result
			mu.Unlock()
		}(peerName, subtask)
	}

	wg.Wait()

	// 整合结果
	summary, err := a.integratePeerResults(ctx, assignments, results)
	if err != nil {
		return "", err
	}

	return summary, firstErr
}

// integratePeerResults 整合对等 MultiAgent 结果
func (a *HybridAgent) integratePeerResults(ctx context.Context, assignments map[string]string, peerResults map[string]string) (string, error) {
	// 构建整合提示
	integratePrompt := fmt.Sprintf(`
请整合以下对等MultiAgent的执行结果，提供最终答案：

任务：%s

对等MultiAgent执行结果：
	`, assignments)

	for peer, result := range peerResults {
		integratePrompt += fmt.Sprintf("%s: %s\n\n", peer, result)
	}

	integratePrompt += "请提供详细的整合报告，包括：\n1. 任务完成情况\n2. 各MultiAgent的主要发现\n3. 最终结论\n4. MultiAgent意见的一致性分析"

	// 生成整合报告
	integrateMessages := []*schema.Message{
		{
			Role:    "system",
			Content: "你是一个结果整合专家，擅长整合多个对等MultiAgent的执行结果，分析MultiAgent意见的一致性，并提供全面的最终报告。",
		},
		{
			Role:    "user",
			Content: integratePrompt,
		},
	}

	return a.runLLMStage(ctx, "integrate", "hybrid_task", integrateMessages, 0, "hybrid_integration", "hybrid_task")
}

// 容错机制相关函数

// RetryWithBackoff 带退避的重试机制
func RetryWithBackoff(maxRetries int, initialDelay time.Duration, fn func() error) error {
	var err error
	for i := 0; i < maxRetries; i++ {
		err = fn()
		if err == nil {
			return nil
		}
		// 指数退避
		delay := initialDelay * time.Duration(1<<i)
		time.Sleep(delay)
	}
	return err
}

// CircuitBreaker 熔断器机制
type CircuitBreaker struct {
	maxFailures int
	failures    int
	lastFailure time.Time
	cooldown    time.Duration
}

// NewCircuitBreaker 创建熔断器
func NewCircuitBreaker(maxFailures int, cooldown time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures: maxFailures,
		cooldown:    cooldown,
	}
}

// AllowRequest 检查是否允许请求
func (cb *CircuitBreaker) AllowRequest() bool {
	if cb.failures >= cb.maxFailures {
		// 检查是否已过冷却期
		if time.Since(cb.lastFailure) >= cb.cooldown {
			cb.failures = 0
			return true
		}
		return false
	}
	return true
}

// RecordFailure 记录失败
func (cb *CircuitBreaker) RecordFailure() {
	cb.failures++
	cb.lastFailure = time.Now()
}

// RecordSuccess 记录成功
func (cb *CircuitBreaker) RecordSuccess() {
	cb.failures = 0
}

// Fallback 降级机制
func Fallback(primary func() (string, error), fallback func() (string, error)) (string, error) {
	result, err := primary()
	if err != nil {
		// 降级到备用方案
		return fallback()
	}
	return result, nil
}

// negotiateTask 协商任务分配
func (a *PeerAgent) negotiateTask(ctx context.Context, task string, sessionStateInfo string) (map[string]string, error) {
	// 构建协商提示
	negotiatePrompt := fmt.Sprintf(`
协商以下任务的分配，确定每个Agent应该执行的子任务：

任务：%s

	%s

可用Agent：
1. data_collector：负责收集股票的基本信息、财务数据和新闻
2. financial_analyzer：负责分析股票的财务状况和指标
3. industry_analyzer：负责分析行业趋势和竞争格局
4. risk_analyzer：负责评估投资风险和对比分析
5. coordinator：负责协调和整合结果

请输出每个Agent的子任务（JSON格式）：
{"data_collector": "收集茅台的财务数据", "financial_analyzer": "分析茅台的财务状况", ...}
	`, task, sessionStateInfo)

	// 协商任务分配
	negotiateMessages := []*schema.Message{
		{
			Role:    "system",
			Content: "你是一个任务协商专家，擅长协调多个Agent的任务分配。",
		},
		{
			Role:    "user",
			Content: negotiatePrompt,
		},
	}
	response, err := a.runLLMStage(ctx, "negotiate", task, negotiateMessages, 0, "peer_negotiation", task)
	if err != nil {
		return nil, err
	}

	// 解析结果
	assignments := make(map[string]string)
	err = json.Unmarshal([]byte(response), &assignments)
	if err != nil {
		return nil, err
	}

	return assignments, nil
}

// executeOwnTask 执行自己的子任务
func (a *PeerAgent) executeOwnTask(ctx context.Context, subtask string) (string, error) {
	if subtask == "" {
		subtask = "执行相关任务"
	}

	// 执行子任务
	return a.Agent.Run(ctx, subtask)
}

// exchangeResults 交换结果
func (a *PeerAgent) exchangeResults(ctx context.Context, assignments map[string]string, ownResult string) (map[string]string, error) {
	results := make(map[string]string)
	results["coordinator"] = ownResult

	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error

	for peerName, subtask := range assignments {
		if peerName == "coordinator" {
			continue
		}

		wg.Add(1)
		go func(name string, task string) {
			defer wg.Done()
			peer, exists := a.peers[name]
			if !exists {
				result := fmt.Sprintf("Agent %s 不存在", name)
				mu.Lock()
				results[name] = result
				mu.Unlock()
				return
			}

			// 执行子任务
			result, err := peer.Run(ctx, task)
			if err != nil {
				result = fmt.Sprintf("执行失败: %v", err)
				if firstErr == nil {
					firstErr = err
				}
			}

			mu.Lock()
			results[name] = result
			mu.Unlock()
		}(peerName, subtask)
	}

	wg.Wait()

	return results, firstErr
}

// NewAgentByMode 根据模式创建 Agent（legacy）
// 已统一到 Coordinator 体系，此函数仅用于向后兼容
func NewAgentByMode(config AgentConfig, mode CoordinatorType) Runner {
	switch mode {
	case CoordinatorTypeSupervisor:
		return NewAgent(config)
	case CoordinatorTypePlan:
		return NewPlanExecuteAgent(config)
	case CoordinatorTypeCommittee:
		return NewMultiAgent(config)
	case CoordinatorTypePeer:
		return NewPeerAgent(config)
	case CoordinatorTypeDeep:
		return NewHybridAgent(config)
	default:
		return NewAgent(config)
	}
}

// NewAgentByModeWithContext 根据模式和任务上下文创建 Agent（legacy）
// 已统一到 Coordinator 体系，此函数仅用于向后兼容
func NewAgentByModeWithContext(config AgentConfig, mode CoordinatorType, taskCtx *pkgctx.TaskContext) Runner {
	var runner Runner
	switch mode {
	case CoordinatorTypeSupervisor:
		runner = NewAgent(config)
	case CoordinatorTypePlan:
		runner = NewPlanExecuteAgent(config)
	case CoordinatorTypeCommittee:
		runner = NewMultiAgent(config)
	case CoordinatorTypePeer:
		runner = NewPeerAgent(config)
	case CoordinatorTypeDeep:
		runner = NewHybridAgent(config)
	default:
		runner = NewAgent(config)
	}

	if runner != nil && taskCtx != nil {
		if agent, ok := runner.(*Agent); ok {
			agent.SetTaskContext(taskCtx)
		} else if agent, ok := runner.(*ReactAgent); ok {
			agent.Agent.SetTaskContext(taskCtx)
		} else if agent, ok := runner.(*PlanExecuteAgent); ok {
			agent.Agent.SetTaskContext(taskCtx)
		} else if agent, ok := runner.(*MultiAgent); ok {
			agent.Agent.SetTaskContext(taskCtx)
		} else if agent, ok := runner.(*EinoMultiAgent); ok {
			agent.Agent.SetTaskContext(taskCtx)
		} else if agent, ok := runner.(*PeerAgent); ok {
			agent.Agent.SetTaskContext(taskCtx)
		} else if agent, ok := runner.(*HybridAgent); ok {
			agent.Agent.SetTaskContext(taskCtx)
		}
	}

	return runner
}
