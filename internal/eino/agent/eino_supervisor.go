package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"stock_rag/internal/concurrency"
)

// Deprecated: EinoSupervisorAgent 已废弃，建议使用 Coordinator 体系（SupervisorCoordinator）
// 此文件仅保留为 legacy fallback，不应继续作为主路径使用
//
// 重复内容说明：
// - ExecuteRequest/Response -> 应使用 TaskState
// - specialistTools map -> 应使用 ToolRegistry
// - analyzeTask 规划逻辑 -> 应使用 Coordinator.Execute
// - executeWorkflow 工作流 -> 应使用 SupervisorCoordinator
//
// 建议迁移到：
//   - Coordinator 接口 (coordinator.go)
//   - SupervisorCoordinator (supervisor_coordinator.go)
//   - ProfileRegistry (profiles.go)
//   - TaskState (task_state.go)

// ExecuteRequest 执行请求
type ExecuteRequest struct {
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	UserID         string `json:"user_id"`
	UserMessage    string `json:"user_message"`
}

// ExecuteResponse 执行响应
type ExecuteResponse struct {
	Success bool   `json:"success"`
	Answer  string `json:"answer"`
}

// EinoSupervisorAgent 使用 Eino ADK 实现的 Supervisor Agent
type EinoSupervisorAgent struct {
	llmClient       *concurrency.LLMClient
	specialistTools map[string]*EinoSpecialistTool
	agent           *Agent
}

// EinoSpecialistTool 包装领域能力为 Eino 工具
type EinoSpecialistTool struct {
	Name        string
	Description string
	InvokeFunc  func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error)
}

// NewEinoSupervisorAgent 创建 Eino Supervisor Agent
func NewEinoSupervisorAgent(agent *Agent) *EinoSupervisorAgent {
	return &EinoSupervisorAgent{
		agent:           agent,
		llmClient:       agent.LLMClient,
		specialistTools: make(map[string]*EinoSpecialistTool),
	}
}

// RegisterSpecialistTool 注册 specialist 工具
func (s *EinoSupervisorAgent) RegisterSpecialistTool(name, description string, invokeFunc func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error)) {
	s.specialistTools[name] = &EinoSpecialistTool{
		Name:       name,
		InvokeFunc: invokeFunc,
	}
}

// Name 返回 agent 名称
func (s *EinoSupervisorAgent) Name() string {
	return "EinoSupervisor"
}

// Execute 执行复杂任务
func (s *EinoSupervisorAgent) Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error) {
	// 创建任务状态
	taskState := &TaskState{
		MessageID:      req.MessageID,
		ConversationID: req.ConversationID,
		UserID:         req.UserID,
		Status:         TaskStatusRunning,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		EvidenceSet:    make([]EvidenceItem, 0),
		MetricTable:    make([]MetricItem, 0),
	}

	// 1. 分析任务，生成执行计划
	plan, err := s.analyzeTask(ctx, req.UserMessage, taskState)
	if err != nil {
		taskState.Status = TaskStatusFailed
		taskState.AddError(err.Error())
		return &ExecuteResponse{
			Success: false,
			Answer:  fmt.Sprintf("任务分析失败: %v", err),
		}, err
	}

	// 2. 构建并执行 Eino 工作流
	result, err := s.executeWorkflow(ctx, req.UserMessage, plan, taskState)
	if err != nil {
		taskState.Status = TaskStatusFailed
		taskState.AddError(err.Error())
		return &ExecuteResponse{
			Success: false,
			Answer:  fmt.Sprintf("工作流执行失败: %v", err),
		}, err
	}

	taskState.Status = TaskStatusCompleted
	taskState.UpdatedAt = time.Now()

	return &ExecuteResponse{
		Success: true,
		Answer:  result,
	}, nil
}

// analyzeTask 分析任务需求，生成执行计划
func (s *EinoSupervisorAgent) analyzeTask(ctx context.Context, task string, taskState *TaskState) (*ExecutionPlan, error) {
	toolList := ""
	for name, tool := range s.specialistTools {
		toolList += fmt.Sprintf("%s: %s\n", name, tool.Description)
	}

	analyzePrompt := fmt.Sprintf(`
分析以下任务，确定执行计划：

任务：%s

可用工具：
%s

请输出详细的执行计划，包括：
1. 是否为复杂任务
2. 需要调用的工具列表
3. 每个步骤的详细说明

格式要求：JSON
{
    "is_complex": true,
    "description": "任务描述",
    "steps": [
        {
            "step": 1,
            "agent": "工具名称",
            "task": "任务描述",
            "input": "输入信息",
            "expected": "预期输出"
        }
    ],
    "requires": ["工具1", "工具2"]
}
`, task, toolList)

	req := &concurrency.LLMRequest{
		RequestID: fmt.Sprintf("analyze-%d", time.Now().UnixNano()),
		Question:  analyzePrompt,
		TaskType:  "task_analysis",
		Priority:  1,
		Timeout:   2 * time.Minute,
		Stream:    false,
	}

	response, err := s.llmClient.Generate(ctx, req)
	if err != nil {
		return nil, err
	}

	return s.parseExecutionPlan(response)
}

// parseExecutionPlan 解析执行计划
func (s *EinoSupervisorAgent) parseExecutionPlan(response string) (*ExecutionPlan, error) {
	var plan ExecutionPlan
	if err := json.Unmarshal([]byte(response), &plan); err != nil {
		return s.parseFallbackPlan(response), nil
	}
	return &plan, nil
}

// parseFallbackPlan 解析非标准格式的执行计划
func (s *EinoSupervisorAgent) parseFallbackPlan(response string) *ExecutionPlan {
	plan := &ExecutionPlan{
		Steps: []ExecutionPlanStep{},
	}

	lines := strings.Split(response, "\n")
	stepNum := 0
	for _, line := range lines {
		if strings.Contains(line, "证据") || strings.Contains(line, "检索") || strings.Contains(line, "文档") {
			stepNum++
			plan.Steps = append(plan.Steps, ExecutionPlanStep{
				StepID: fmt.Sprintf("%d", stepNum),
				Agent:  "evidence_collector",
				Tool:   "retrieve_evidence",
				Input:  map[string]interface{}{"query": line},
			})
		} else if strings.Contains(line, "指标") || strings.Contains(line, "财务") || strings.Contains(line, "数据") {
			stepNum++
			plan.Steps = append(plan.Steps, ExecutionPlanStep{
				StepID: fmt.Sprintf("%d", stepNum),
				Agent:  "metric_extractor",
				Tool:   "extract_metrics",
				Input:  map[string]interface{}{"query": line},
			})
		} else if strings.Contains(line, "分析") || strings.Contains(line, "报告") || strings.Contains(line, "总结") {
			stepNum++
			plan.Steps = append(plan.Steps, ExecutionPlanStep{
				StepID: fmt.Sprintf("%d", stepNum),
				Agent:  "analyst_writer",
				Tool:   "generate_report",
				Input:  map[string]interface{}{"query": line},
			})
		}
	}

	if len(plan.Steps) == 0 {
		plan.Steps = append(plan.Steps, ExecutionPlanStep{
			StepID: "1",
			Agent:  "evidence_collector",
			Tool:   "retrieve_evidence",
			Input:  map[string]interface{}{"query": response},
		})
		plan.Steps = append(plan.Steps, ExecutionPlanStep{
			StepID: "2",
			Agent:  "metric_extractor",
			Tool:   "extract_metrics",
			Input:  map[string]interface{}{},
		})
		plan.Steps = append(plan.Steps, ExecutionPlanStep{
			StepID: "3",
			Agent:  "analyst_writer",
			Tool:   "generate_report",
			Input:  map[string]interface{}{},
		})
	}

	return plan
}

// executeWorkflow 执行工作流
func (s *EinoSupervisorAgent) executeWorkflow(ctx context.Context, task string, plan *ExecutionPlan, taskState *TaskState) (string, error) {
	if len(plan.Steps) == 0 {
		return "没有可执行的步骤", nil
	}

	// 创建工作流

	// 初始化上下文
	workflowContext := map[string]interface{}{
		"task":       task,
		"plan":       plan,
		"task_state": taskState,
	}

	// 顺序执行步骤
	for i, step := range plan.Steps {
		stepNum := i + 1

		// 更新任务状态
		taskState.CurrentStep = stepNum
		taskState.UpdatedAt = time.Now()

		// 调用对应的工具
		toolName := step.Agent
		tool, exists := s.specialistTools[toolName]
		if !exists {
			return fmt.Sprintf("工具 %s 不存在", toolName), nil
		}

		// 准备工具输入
		toolInput := map[string]interface{}{
			"task":         task,
			"step":         step,
			"evidence_set": taskState.EvidenceSet,
			"metric_table": taskState.MetricTable,
		}

		// 调用工具
		result, err := tool.InvokeFunc(ctx, toolInput)
		if err != nil {
			return fmt.Sprintf("步骤 %d 执行失败: %v", stepNum, err), nil
		}

		// 更新任务状态
		if report, ok := result["report"].(string); ok {
			workflowContext["final_report"] = report
		}

		workflowContext["step_result"] = result
	}

	// 提取最终结果
	if finalReport, ok := workflowContext["final_report"].(string); ok {
		return finalReport, nil
	}

	return fmt.Sprintf("任务执行完成，共执行 %d 个步骤", len(plan.Steps)), nil
}

// GetStepTraces 获取步骤追踪信息
func (s *EinoSupervisorAgent) GetStepTraces() []StepTrace {
	if s.agent != nil {
		return s.agent.StepTraces
	}
	return nil
}

// GetTaskState 获取任务状态
func (s *EinoSupervisorAgent) GetTaskState(taskID string) (*TaskState, error) {
	return nil, fmt.Errorf("状态查询功能尚未实现")
}
