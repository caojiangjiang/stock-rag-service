package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"stock_rag/internal/router"
	"stock_rag/internal/service"
)

// PlannerAgent 规划/监督 Agent
type PlannerAgent struct {
	name          string
	evidenceAgent *EvidenceAgent
	metricAgent   *MetricAgent
	analystAgent  *AnalystAgent
}

func NewPlannerAgent(evidenceAgent *EvidenceAgent, metricAgent *MetricAgent, analystAgent *AnalystAgent) *PlannerAgent {
	return &PlannerAgent{
		name:          "planner_agent",
		evidenceAgent: evidenceAgent,
		metricAgent:   metricAgent,
		analystAgent:  analystAgent,
	}
}

func (a *PlannerAgent) Name() string {
	return a.name
}

func (a *PlannerAgent) Mode() router.RouteMode {
	return router.ModeAgent
}

func (a *PlannerAgent) Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error) {
	// 第一步：分析问题，生成执行计划
	plan := a.analyzeQuestion(req.UserMessage, req.StockCode)

	if !plan.IsComplex {
		// 简单问题，直接使用 Evidence + Analyst
		return a.executeSimpleFlow(ctx, req, plan)
	}

	// 复杂问题，执行完整流程
	return a.executeComplexFlow(ctx, req, plan)
}

// 分析问题并生成执行计划
func (a *PlannerAgent) analyzeQuestion(message, stockCode string) *ExecutionPlan {
	plan := &ExecutionPlan{
		Steps:       make([]PlanStep, 0),
		Requires:    make([]string, 0),
		IsComplex:   false,
		Description: "简单查询",
	}

	// 判断问题类型
	if a.isComparisonQuestion(message) {
		plan.IsComplex = true
		plan.Description = "对比分析问题"
		plan.Requires = append(plan.Requires, "evidence", "metric", "analyst")
	} else if a.isMultiTimeQuestion(message) {
		plan.IsComplex = true
		plan.Description = "多时间维度问题"
		plan.Requires = append(plan.Requires, "evidence", "metric", "analyst")
	} else if a.isMultiSourceQuestion(message) {
		plan.IsComplex = true
		plan.Description = "多来源交叉验证问题"
		plan.Requires = append(plan.Requires, "evidence", "metric", "analyst")
	} else if a.isSimpleFactQuestion(message) {
		plan.IsComplex = false
		plan.Description = "简单事实查询"
		plan.Requires = append(plan.Requires, "evidence", "analyst")
	} else {
		plan.IsComplex = false
		plan.Description = "常规问题"
		plan.Requires = append(plan.Requires, "evidence", "analyst")
	}

	// 构建步骤
	step := 1
	if contains(plan.Requires, "evidence") {
		plan.Steps = append(plan.Steps, PlanStep{
			Step:     step,
			Agent:    "evidence_agent",
			Task:     "检索相关文档",
			Input:    message,
			Expected: "返回结构化证据集合",
		})
		step++
	}

	if contains(plan.Requires, "metric") {
		plan.Steps = append(plan.Steps, PlanStep{
			Step:     step,
			Agent:    "metric_agent",
			Task:     "提取财务指标",
			Input:    "证据集合",
			Expected: "返回表格化财务数据",
		})
		step++
	}

	if contains(plan.Requires, "analyst") {
		plan.Steps = append(plan.Steps, PlanStep{
			Step:     step,
			Agent:    "analyst_agent",
			Task:     "生成最终回答",
			Input:    "证据+指标",
			Expected: "返回最终分析报告",
		})
	}

	return plan
}

// 判断是否是对比分析问题
func (a *PlannerAgent) isComparisonQuestion(message string) bool {
	keywords := []string{"对比", "相比", "比较", "vs", "和", "与"}
	for _, kw := range keywords {
		if strings.Contains(message, kw) {
			// 检查是否有多个公司或指标
			companyPattern := regexp.MustCompile(`(茅台|五粮液|宁德时代|比亚迪|腾讯|阿里巴巴|美团|京东)`)
			matches := companyPattern.FindAllString(message, -1)
			if len(matches) >= 2 {
				return true
			}
		}
	}
	return false
}

// 判断是否是多时间维度问题
func (a *PlannerAgent) isMultiTimeQuestion(message string) bool {
	yearPattern := regexp.MustCompile(`(\d{4})年`)
	matches := yearPattern.FindAllString(message, -1)
	return len(matches) >= 2
}

// 判断是否是多来源交叉验证问题
func (a *PlannerAgent) isMultiSourceQuestion(message string) bool {
	keywords := []string{"验证", "确认", "核对", "交叉", "多方", "多个来源"}
	for _, kw := range keywords {
		if strings.Contains(message, kw) {
			return true
		}
	}
	return false
}

// 判断是否是简单事实问题
func (a *PlannerAgent) isSimpleFactQuestion(message string) bool {
	patterns := []string{
		`(\d{4})年.*(营业收入|净利润|现金流|资产|负债)`,
		`(茅台|五粮液|宁德时代|比亚迪).*(\d{4})年.*(多少|是多少)`,
	}

	for _, pattern := range patterns {
		if matched, _ := regexp.MatchString(pattern, message); matched {
			return true
		}
	}
	return false
}

// 执行简单流程
func (a *PlannerAgent) executeSimpleFlow(ctx context.Context, req *ExecuteRequest, plan *ExecutionPlan) (*ExecuteResponse, error) {
	// 1. 获取证据
	evidenceResp, err := a.evidenceAgent.Execute(ctx, &SpecialistRequest{
		UserMessage: req.UserMessage,
		StockCode:   req.StockCode,
	})
	if err != nil || !evidenceResp.Success {
		return &ExecuteResponse{
			Content: fmt.Sprintf("证据检索失败: %s", evidenceResp.Error),
			Mode:    router.ModeAgent,
		}, nil
	}

	// 2. 直接调用 Analyst
	analystResp, err := a.analystAgent.Execute(ctx, &SpecialistRequest{
		UserMessage: req.UserMessage,
		StockCode:   req.StockCode,
		EvidenceSet: evidenceResp.EvidenceSet,
		Plan:        plan,
	})
	if err != nil || !analystResp.Success {
		return &ExecuteResponse{
			Content: fmt.Sprintf("分析失败: %s", analystResp.Error),
			Mode:    router.ModeAgent,
		}, nil
	}

	return &ExecuteResponse{
		Content:   analystResp.Summary,
		Mode:      router.ModeAgent,
		Citations: a.buildCitations(evidenceResp.EvidenceSet),
	}, nil
}

// 执行复杂流程
func (a *PlannerAgent) executeComplexFlow(ctx context.Context, req *ExecuteRequest, plan *ExecutionPlan) (*ExecuteResponse, error) {
	var evidenceSet *EvidenceSet
	var metricTable *MetricTable

	// 按照计划步骤执行
	for _, step := range plan.Steps {
		switch step.Agent {
		case "evidence_agent":
			resp, err := a.evidenceAgent.Execute(ctx, &SpecialistRequest{
				UserMessage: req.UserMessage,
				StockCode:   req.StockCode,
			})
			if err != nil || !resp.Success {
				return &ExecuteResponse{
					Content: fmt.Sprintf("步骤%d失败 [%s]: %s", step.Step, step.Agent, resp.Error),
					Mode:    router.ModeAgent,
				}, nil
			}
			evidenceSet = resp.EvidenceSet

		case "metric_agent":
			if evidenceSet == nil {
				return &ExecuteResponse{
					Content: fmt.Sprintf("步骤%d失败 [%s]: 缺少证据数据", step.Step, step.Agent),
					Mode:    router.ModeAgent,
				}, nil
			}
			resp, err := a.metricAgent.Execute(ctx, &SpecialistRequest{
				UserMessage: req.UserMessage,
				StockCode:   req.StockCode,
				EvidenceSet: evidenceSet,
			})
			if err != nil || !resp.Success {
				return &ExecuteResponse{
					Content: fmt.Sprintf("步骤%d失败 [%s]: %s", step.Step, step.Agent, resp.Error),
					Mode:    router.ModeAgent,
				}, nil
			}
			metricTable = resp.MetricTable

		case "analyst_agent":
			resp, err := a.analystAgent.Execute(ctx, &SpecialistRequest{
				UserMessage: req.UserMessage,
				StockCode:   req.StockCode,
				EvidenceSet: evidenceSet,
				MetricTable: metricTable,
				Plan:        plan,
			})
			if err != nil || !resp.Success {
				return &ExecuteResponse{
					Content: fmt.Sprintf("步骤%d失败 [%s]: %s", step.Step, step.Agent, resp.Error),
					Mode:    router.ModeAgent,
				}, nil
			}

			return &ExecuteResponse{
				Content:   resp.Summary,
				Mode:      router.ModeAgent,
				Citations: a.buildCitations(evidenceSet),
			}, nil
		}
	}

	return &ExecuteResponse{
		Content: "执行完成，但未生成最终回答",
		Mode:    router.ModeAgent,
	}, nil
}

func (a *PlannerAgent) buildCitations(evidenceSet *EvidenceSet) []Citation {
	citations := make([]Citation, 0)
	if evidenceSet != nil {
		for _, item := range evidenceSet.Items {
			citations = append(citations, Citation{
				StockCode: item.StockCode,
				DocType:   item.DocType,
				Title:     item.Title,
				Content:   item.Content,
				Score:     item.Confidence,
			})
		}
	}
	return citations
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// ExecuteComplexTask 实现 service.SupervisorAgent 接口
func (a *PlannerAgent) ExecuteComplexTask(ctx context.Context, req *service.ComplexTaskExecuteRequest) (*service.ComplexTaskResponse, error) {
	executeReq := &ExecuteRequest{
		ConversationID: req.ConversationID,
		MessageID:      req.MessageID,
		UserID:         req.UserID,
		UserMessage:    req.UserMessage,
		StockCode:      req.StockCode,
	}

	resp, err := a.Execute(ctx, executeReq)
	if err != nil {
		return &service.ComplexTaskResponse{
			MessageID: req.MessageID,
			Error:     err.Error(),
		}, nil
	}

	return &service.ComplexTaskResponse{
		MessageID:    resp.MessageID,
		Content:      resp.Content,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		LatencyMs:    resp.LatencyMs,
	}, nil
}
