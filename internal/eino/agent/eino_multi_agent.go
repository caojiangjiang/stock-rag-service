package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"stock_rag/internal/concurrency"
)

// EinoMultiAgent 使用 Eino ADK 实现的多 Agent 模式
type EinoMultiAgent struct {
	*Agent
	LLMClient   *concurrency.LLMClient
	specialists map[string]*Agent
	runner      *adk.Runner
}

// NewEinoMultiAgent 创建使用 Eino ADK 的多 Agent
func NewEinoMultiAgent(config AgentConfig) *EinoMultiAgent {
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

	return &EinoMultiAgent{
		Agent:       baseAgent,
		LLMClient:   baseAgent.LLMClient,
		specialists: specialists,
	}
}

// Run 执行 Eino 多 Agent 模式
func (a *EinoMultiAgent) Run(ctx context.Context, task string) (string, error) {
	runtime := a.beginTask(task)

	// 1. 分析任务需求，确定需要的专家
	experts, subtasks, err := a.analyzeTask(ctx, task, runtime.TaskContextInfo)
	if err != nil {
		return "", err
	}

	// 2. 构建 Eino 工作流
	wf := a.buildWorkflow(ctx, experts, subtasks)

	// 3. 执行工作流
	result, err := wf.Invoke(ctx, map[string]any{
		"task":          task,
		"session_state": runtime.TaskContextInfo,
		"experts":       experts,
		"subtasks":      subtasks,
	})
	if err != nil {
		return "", err
	}

	// 4. 处理结果
	summary, ok := result.(string)
	if !ok {
		return "", fmt.Errorf("工作流返回类型错误")
	}

	a.completeTask(summary)
	return summary, nil
}

// buildWorkflow 构建 Eino 工作流
func (a *EinoMultiAgent) buildWorkflow(ctx context.Context, experts []string, subtasks map[string]string) compose.Runnable[map[string]any, any] {
	// 创建 Workflow
	wf := compose.NewWorkflow[map[string]any, any]()

	// 添加任务分析节点
	taskAnalysisNode := wf.AddLambdaNode("task_analysis", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (map[string]any, error) {
		return input, nil
	}))
	taskAnalysisNode.AddInput(compose.START)

	// 添加专家执行节点（并行）
	expertExecutionNode := wf.AddLambdaNode("expert_execution", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (map[string]any, error) {
		experts := input["experts"].([]string)
		subtasks := input["subtasks"].(map[string]string)
		results := make(map[string]string)

		// 并行执行专家任务
		var wg sync.WaitGroup
		var mu sync.Mutex
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
						StepID:      fmt.Sprintf("%d", idx + 1),
						ToolName:    fmt.Sprintf("expert:%s", name),
						Input:       map[string]interface{}{"task": subtasks[name]},
						Output:      observation,
						EndTime:     time.Now(),
						Status:      TaskStatusFailed,
						StartTime:   stepStart,
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

				a.syncSpecialistRuntime(expert)

				// 执行子任务
				result, err := expert.Run(expertCtx, subtask)
				a.syncParentRuntimeState(expert)
				if err != nil {
					result = fmt.Sprintf("执行失败: %v", err)
					if firstErr == nil {
						firstErr = err
					}
				}

				mu.Lock()
				a.appendMemoryMessage(&schema.Message{
					Role:    "tool",
					Content: fmt.Sprintf("专家 %s 子任务执行结果: %s", name, result),
				})
				a.appendStepTrace(StepTrace{
					StepID:      fmt.Sprintf("%d", idx + 1),
					ToolName:    fmt.Sprintf("expert:%s", name),
					Input:       map[string]interface{}{"task": subtask},
					Output:      result,
					EndTime:     time.Now(),
					Status:      TaskStatusCompleted,
					StartTime:   stepStart,
				})
				results[name] = result
				mu.Unlock()
			}(i, expertName)
		}

		wg.Wait()
		input["expert_results"] = results
		return input, firstErr
	}))
	expertExecutionNode.AddInput("task_analysis")

	// 添加结果整合节点
	resultIntegrationNode := wf.AddLambdaNode("result_integration", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (string, error) {
		task := input["task"].(string)
		sessionStateInfo := input["session_state"].(string)
		expertResults := input["expert_results"].(map[string]string)

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

		return a.runLLMStage(ctx, "integrate", task, integrateMessages, len(expertResults)+1, "expert_integration", task)
	}))
	resultIntegrationNode.AddInput("expert_execution")

	// 结束节点
	wf.End().AddInput("result_integration")

	// 编译工作流
	runnable, err := wf.Compile(ctx)
	if err != nil {
		panic(err)
	}

	return runnable
}

// analyzeTask 分析任务需求
func (a *EinoMultiAgent) analyzeTask(ctx context.Context, task string, sessionStateInfo string) ([]string, map[string]string, error) {
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

func (a *EinoMultiAgent) syncSpecialistRuntime(expert *Agent) {
	expert.LLMClient = a.LLMClient
	expert.TaskContext = a.TaskContext
	expert.Memory = a.Memory
	expert.StepTraces = a.StepTraces
	expert.ToolFailures = a.ToolFailures
}

func (a *EinoMultiAgent) syncParentRuntimeState(expert *Agent) {
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
