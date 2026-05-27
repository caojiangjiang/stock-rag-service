package agent

import (
	"context"
	"fmt"
	"time"

	"stock_rag/internal/eino/adapter"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

type WorkflowCoordinator struct {
	*BaseCoordinator
}

func NewWorkflowCoordinator(profileRegistry *ProfileRegistry, agentBuilder *AgentBuilder) *WorkflowCoordinator {
	base := NewBaseCoordinator("workflow", profileRegistry, agentBuilder)
	return &WorkflowCoordinator{
		BaseCoordinator: base,
	}
}

func (c *WorkflowCoordinator) Execute(ctx context.Context, taskState *TaskState) (string, error) {
	profiles := c.GetAgentProfiles()
	if len(profiles) == 0 {
		return "没有配置任何 Agent", nil
	}

	rt := RuntimeFromContext(ctx)
	if rt == nil {
		rt = NewCoordinatorRuntime(c.Name(), nil)
	}
	ctx = WithRuntime(ctx, rt)
	ctx, endSpan := StartCoordinatorSpan(ctx, c.Name(), taskState)
	defer endSpan()

	coordinatorStart := time.Now()
	defer func() {
		status := "success"
		if taskState.Status == TaskStatusFailed {
			status = "error"
		}
		classifier := taskState.ClassifierType
		if classifier == "" {
			classifier = "unknown"
		}
		RecordCoordinatorResult(c.Name(), classifier, status, time.Since(coordinatorStart).Seconds())
	}()

	runCtx, cancel := rt.DeriveContext(ctx)
	defer cancel()

	taskState.UpdateStatus(TaskStatusRunning)

	workflow := c.buildWorkflowGraph(profiles, taskState)

	runnable, err := workflow.Compile(runCtx)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("编译工作流失败: %v", err))
		return "", err
	}

	input := map[string]any{
		"task":         taskState.UserMessage,
		"stock_code":   taskState.StockCode,
		"company_name": taskState.CompanyName,
		"time_range":   taskState.TimeRange,
		"doc_types":    taskState.DocTypes,
	}

	result, err := runnable.Invoke(runCtx, input)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("工作流执行失败: %v", err))
		return "", err
	}

	var output string
	if r, ok := result.(string); ok {
		output = r
	} else {
		output = fmt.Sprintf("%v", result)
	}

	taskState.UpdateStatus(TaskStatusCompleted)
	taskState.Summary = output

	return output, nil
}

func (c *WorkflowCoordinator) buildWorkflowGraph(profiles []*AgentProfile, taskState *TaskState) *compose.Workflow[map[string]any, any] {
	rt := NewCoordinatorRuntime(c.Name(), nil)
	wf := compose.NewWorkflow[map[string]any, any]()

	evidenceProfile := findProfileByName(profiles, "evidence_collector")
	metricsProfile := findProfileByName(profiles, "metric_extractor")
	analystProfile := findProfileByName(profiles, "analyst_writer")

	if evidenceProfile == nil || metricsProfile == nil || analystProfile == nil {
		return c.buildSimpleWorkflow(profiles, taskState, rt)
	}

	evidenceNode := wf.AddLambdaNode("evidence_collector", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (map[string]any, error) {
		task := getStringValue(input, "task")
		stockCode := getStringValue(input, "stock_code")
		companyName := getStringValue(input, "company_name")

		evidence, err := rt.RunSubTask(ctx, taskState, "evidence_collector", func(stepCtx context.Context) (string, error) {
			return c.executeEvidenceRetrieval(stepCtx, evidenceProfile, task, stockCode, companyName)
		})
		if err != nil {
			return nil, fmt.Errorf("evidence retrieval failed: %w", err)
		}

		return map[string]any{
			"evidence":     evidence,
			"task":         task,
			"stock_code":   stockCode,
			"company_name": companyName,
		}, nil
	}))
	evidenceNode.AddInput(compose.START)

	metricsNode := wf.AddLambdaNode("metric_extractor", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (map[string]any, error) {
		task := getStringValue(input, "task")
		evidence := getStringValue(input, "evidence")
		stockCode := getStringValue(input, "stock_code")

		metrics, err := rt.RunSubTask(ctx, taskState, "metric_extractor", func(stepCtx context.Context) (string, error) {
			return c.executeMetricExtraction(stepCtx, metricsProfile, task, evidence, stockCode)
		})
		if err != nil {
			return nil, fmt.Errorf("metric extraction failed: %w", err)
		}

		return map[string]any{
			"evidence": evidence,
			"metrics":  metrics,
			"task":     task,
		}, nil
	}))
	metricsNode.AddInput("evidence_collector")

	analystNode := wf.AddLambdaNode("analyst_writer", compose.InvokableLambda(func(ctx context.Context, input map[string]any) (string, error) {
		evidence := getStringValue(input, "evidence")
		metrics := getStringValue(input, "metrics")
		task := getStringValue(input, "task")

		report, err := rt.RunSubTask(ctx, taskState, "analyst_writer", func(stepCtx context.Context) (string, error) {
			return c.executeAnalysis(stepCtx, analystProfile, task, evidence, metrics)
		})
		if err != nil {
			return "", fmt.Errorf("analysis failed: %w", err)
		}

		return report, nil
	}))
	analystNode.AddInput("evidence_collector")
	analystNode.AddInput("metric_extractor")
	wf.End().AddInput("analyst_writer")

	return wf
}

func (c *WorkflowCoordinator) buildSimpleWorkflow(profiles []*AgentProfile, taskState *TaskState, rt *CoordinatorRuntime) *compose.Workflow[map[string]any, any] {
	wf := compose.NewWorkflow[map[string]any, any]()

	for _, profile := range profiles {
		nodeName := profile.Name
		p := profile
		node := wf.AddLambdaNode(nodeName, compose.InvokableLambda(func(ctx context.Context, input map[string]any) (string, error) {
			return rt.RunSubTask(ctx, taskState, p.Name, func(stepCtx context.Context) (string, error) {
				return c.executeSingleAgent(stepCtx, p, input)
			})
		}))
		node.AddInput(compose.START)
		wf.End().AddInput(nodeName)
	}

	return wf
}

func (c *WorkflowCoordinator) executeEvidenceRetrieval(ctx context.Context, profile *AgentProfile, task, stockCode, companyName string) (string, error) {
	instruction := fmt.Sprintf(`你是一位专业的文档检索专家。任务：从企业年报、公告、新闻等文档中检索与用户问题相关的证据信息。

用户问题：%s
股票代码：%s
公司名称：%s

请检索相关文档证据，并按以下格式输出：
1. 证据标题
2. 证据来源/发布时间
3. 证据内容摘要（150字以内）
4. 相关性评分

输出格式示例：
【证据1】
标题：xxx
来源：xxx
摘要：xxx
相关性：0.xx`, task, stockCode, companyName)

	messages := []*schema.Message{
		{Role: schema.System, Content: instruction},
		{Role: schema.User, Content: task},
	}

	// 使用 EinoModelAdapter 包装 llm.GetLLMClient()
	modelAdapter := adapter.NewEinoModelAdapter()
	resp, err := modelAdapter.Generate(ctx, messages)
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

func (c *WorkflowCoordinator) executeMetricExtraction(ctx context.Context, profile *AgentProfile, task, evidence, stockCode string) (string, error) {
	instruction := fmt.Sprintf(`你是一位财务指标专家。任务：从证据文档中提取关键的财务指标数据。

用户问题：%s
股票代码：%s

已检索到的证据：
%s

请提取以下类型的财务指标（单位：人民币）：
- 营业收入及同比变化
- 净利润及同比变化
- 净资产收益率(ROE)
- 资产负债率
- 毛利率
- 每股收益

输出格式示例：
【财务指标】
营业收入：XXX亿元（同比+XX%%）
净利润：XXX亿元（同比+XX%%）
净资产收益率：XX%%
...`, task, stockCode, evidence)

	messages := []*schema.Message{
		{Role: schema.System, Content: instruction},
		{Role: schema.User, Content: "请提取财务指标"},
	}

	// 使用 EinoModelAdapter 包装 llm.GetLLMClient()
	modelAdapter := adapter.NewEinoModelAdapter()
	resp, err := modelAdapter.Generate(ctx, messages)
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

func (c *WorkflowCoordinator) executeAnalysis(ctx context.Context, profile *AgentProfile, task, evidence, metrics string) (string, error) {
	instruction := fmt.Sprintf(`你是一位专业的投资分析师。任务：基于证据和财务指标，生成投资分析报告。

用户问题：%s

已收集的证据：
%s

已提取的财务指标：
%s

请生成一份结构化的投资分析报告，包括：
1. 执行摘要
2. 证据分析
3. 财务指标解读
4. 风险提示
5. 投资建议

报告要求：
- 客观、数据驱动
- 引用证据时标注来源
- 风险提示要具体`, task, evidence, metrics)

	messages := []*schema.Message{
		{Role: schema.System, Content: instruction},
		{Role: schema.User, Content: "请生成分析报告"},
	}

	// 使用 EinoModelAdapter 包装 llm.GetLLMClient()
	modelAdapter := adapter.NewEinoModelAdapter()
	resp, err := modelAdapter.Generate(ctx, messages)
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

func (c *WorkflowCoordinator) executeSingleAgent(ctx context.Context, profile *AgentProfile, input map[string]any) (string, error) {
	task := getStringValue(input, "task")
	instruction := fmt.Sprintf("%s\n\n%s", profile.Role, profile.RolePrompt)
	if len(profile.Constraints) > 0 {
		instruction += "\n\n约束：\n"
		for i, constraint := range profile.Constraints {
			instruction += fmt.Sprintf("%d. %s\n", i+1, constraint)
		}
	}

	messages := []*schema.Message{
		{Role: schema.System, Content: instruction},
		{Role: schema.User, Content: task},
	}

	// 使用 EinoModelAdapter 包装 llm.GetLLMClient()
	modelAdapter := adapter.NewEinoModelAdapter()
	resp, err := modelAdapter.Generate(ctx, messages)
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

func findProfileByName(profiles []*AgentProfile, name string) *AgentProfile {
	for _, p := range profiles {
		if p.Name == name {
			return p
		}
	}
	return nil
}

func getStringValue(input map[string]any, key string) string {
	if v, ok := input[key].(string); ok {
		return v
	}
	return ""
}
