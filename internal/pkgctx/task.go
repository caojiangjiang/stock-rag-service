package pkgctx

import (
	"time"
)

// TaskContext 任务上下文
type TaskContext struct {
	ConversationID      string               `json:"conversation_id"`
	SessionID           string               `json:"session_id"`
	StockCode           string               `json:"stock_code"`
	CompanyName         string               `json:"company_name"`
	TimeRange           string               `json:"time_range"`
	DocTypes            []string             `json:"doc_types"`
	CompareYears        []string             `json:"compare_years"`
	LastUserIntent      string               `json:"last_user_intent"`
	CustomFilters       map[string]string    `json:"custom_filters"`
	OutputFormat        string               `json:"output_format"`
	MaxResults          int                  `json:"max_results"`
	ConfidenceThreshold float64              `json:"confidence_threshold"`
	IsComparison        bool                 `json:"is_comparison"`
	TargetCompanies     []string             `json:"target_companies"`
	Metrics             []string             `json:"metrics"`
	AnalyzeDimensions   []string             `json:"analyze_dimensions"`
	ExecutionPlan       string               `json:"execution_plan"`
	StepResults         []TaskStepResult     `json:"step_results"`
	CreatedAt           time.Time            `json:"created_at"`
	UpdatedAt           time.Time            `json:"updated_at"`
	ConversationSummary *ConversationSummary `json:"conversation_summary"`
}

// ConversationSummary 对话摘要
type ConversationSummary struct {
	CurrentObject    string   `json:"current_object"`
	TimeRange        string   `json:"time_range"`
	DocTypes         []string `json:"doc_types"`
	ConfirmedFacts   []string `json:"confirmed_facts"`
	PendingQuestions []string `json:"pending_questions"`
}

// TaskStepResult 任务步骤结果
type TaskStepResult struct {
	Step     int    `json:"step"`
	ToolName string `json:"tool_name"`
	Args     string `json:"args"`
	Result   string `json:"result"`
	Success  bool   `json:"success"`
	Latency  int    `json:"latency_ms"`
}

// ParseStringSlice 解析字符串切片
func ParseStringSlice(value interface{}) []string {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case []string:
		return v
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

// NewTaskContext 创建新的任务上下文
func NewTaskContext() *TaskContext {
	return &TaskContext{
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}
