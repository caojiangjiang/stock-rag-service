package agent

import (
	"encoding/json"
	"fmt"
	"time"
)

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusReplan    TaskStatus = "replan"
)

type ExecutionPlan struct {
	Steps []ExecutionPlanStep `json:"steps"`
}

type ExecutionPlanStep struct {
	StepID    string                 `json:"step_id"`
	Agent     string                 `json:"agent"`
	Tool      string                 `json:"tool"`
	Input     map[string]interface{} `json:"input"`
	DependsOn []string               `json:"depends_on,omitempty"`
}

type EvidenceItem struct {
	ID          string                 `json:"id"`
	Content     string                 `json:"content"`
	Source      string                 `json:"source"`
	Score       float64                `json:"score"`
	Metadata    map[string]interface{} `json:"metadata"`
	DocType     string                 `json:"doc_type"`
	PublishedAt string                 `json:"published_at"`
}

type MetricItem struct {
	Name       string  `json:"name"`
	Value      float64 `json:"value"`
	Unit       string  `json:"unit"`
	Year       string  `json:"year"`
	Source     string  `json:"source"`
	Confidence float64 `json:"confidence"`
}

type StepTrace struct {
	StepID      string                 `json:"step_id"`
	ToolName    string                 `json:"tool_name"`
	Input       map[string]interface{} `json:"input"`
	Output      string                 `json:"output"`
	Error       string                 `json:"error,omitempty"`
	StartTime   time.Time              `json:"start_time"`
	EndTime     time.Time              `json:"end_time"`
	Status      TaskStatus             `json:"status"`
	Confidence  float64                `json:"confidence"`
	// 可观测性字段
	TraceID     string  `json:"trace_id,omitempty"`
	SpanID      string  `json:"span_id,omitempty"`
	LatencyMS   int64   `json:"latency_ms,omitempty"`
	TokenIn     int     `json:"token_in,omitempty"`
	TokenOut    int     `json:"token_out,omitempty"`
	CostUSD     float64 `json:"cost_usd,omitempty"`
	ModelVersion string `json:"model_version,omitempty"`
}

type Citation struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	PublishedAt string `json:"published_at"`
	Page        int    `json:"page,omitempty"`
}

type TaskState struct {
	ConversationID       string         `json:"conversation_id"`
	MessageID            string         `json:"message_id"`
	UserID               string         `json:"user_id"`
	UserMessage          string         `json:"user_message"`
	StockCode            string         `json:"stock_code"`
	CompanyName          string         `json:"company_name"`
	TimeRange            string         `json:"time_range"`
	DocTypes             []string       `json:"doc_types"`
	Plan                 *ExecutionPlan `json:"plan,omitempty"`
	SelectedTools        []string       `json:"selected_tools"`
	EvidenceSet          []EvidenceItem `json:"evidence_set"`
	MetricTable          []MetricItem   `json:"metric_table"`
	IntermediateFindings []string       `json:"intermediate_findings"`
	Summary              string         `json:"summary"`
	Citations            []Citation     `json:"citations"`
	StepTraces           []StepTrace    `json:"step_traces"`
	Errors               []string       `json:"errors"`
	Status               TaskStatus     `json:"status"`
	CurrentStep          int            `json:"current_step"`
	NeedReplan           bool           `json:"need_replan"`
	RetryCount           int            `json:"retry_count"`
	CheckPointID         string         `json:"check_point_id,omitempty"`
	LastCheckPointStep   string         `json:"last_check_point_step,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

func NewTaskState(conversationID, messageID, userID, userMessage string) *TaskState {
	now := time.Now()
	return &TaskState{
		ConversationID:       conversationID,
		MessageID:            messageID,
		UserID:               userID,
		UserMessage:          userMessage,
		Status:               TaskStatusPending,
		EvidenceSet:          make([]EvidenceItem, 0),
		MetricTable:          make([]MetricItem, 0),
		IntermediateFindings: make([]string, 0),
		Citations:            make([]Citation, 0),
		StepTraces:           make([]StepTrace, 0),
		Errors:               make([]string, 0),
		CreatedAt:            now,
		UpdatedAt:            now,
	}
}

func (s *TaskState) UpdateStatus(status TaskStatus) {
	s.Status = status
	s.UpdatedAt = time.Now()
}

func (s *TaskState) AddEvidence(items ...EvidenceItem) {
	s.EvidenceSet = append(s.EvidenceSet, items...)
	s.UpdatedAt = time.Now()
}

func (s *TaskState) AddMetrics(items ...MetricItem) {
	s.MetricTable = append(s.MetricTable, items...)
	s.UpdatedAt = time.Now()
}

func (s *TaskState) AddFinding(finding string) {
	s.IntermediateFindings = append(s.IntermediateFindings, finding)
	s.UpdatedAt = time.Now()
}

func (s *TaskState) AddCitation(citations ...Citation) {
	s.Citations = append(s.Citations, citations...)
	s.UpdatedAt = time.Now()
}

func (s *TaskState) AddStepTrace(trace StepTrace) {
	s.StepTraces = append(s.StepTraces, trace)
	s.CurrentStep = len(s.StepTraces)
	s.UpdatedAt = time.Now()
}

func (s *TaskState) AddError(err string) {
	s.Errors = append(s.Errors, err)
	s.UpdatedAt = time.Now()
}

func (s *TaskState) SetPlan(plan *ExecutionPlan) {
	s.Plan = plan
	s.UpdatedAt = time.Now()
}

func (s *TaskState) MarkReplan() {
	s.NeedReplan = true
	s.Status = TaskStatusReplan
	s.UpdatedAt = time.Now()
}

func (s *TaskState) GetTotalConfidence() float64 {
	if len(s.EvidenceSet) == 0 {
		return 0
	}
	total := 0.0
	for _, ev := range s.EvidenceSet {
		total += ev.Score
	}
	return total / float64(len(s.EvidenceSet))
}

func (s *TaskState) IsCompleted() bool {
	return s.Status == TaskStatusCompleted || s.Status == TaskStatusFailed
}

func (s *TaskState) GetExecutionSummary() string {
	if s.Summary != "" {
		return s.Summary
	}
	if len(s.IntermediateFindings) > 0 {
		result := ""
		for _, finding := range s.IntermediateFindings {
			result += "- " + finding + "\n"
		}
		return result
	}
	return "执行进行中..."
}

func (s *TaskState) ToJSON() string {
	data, _ := json.Marshal(s)
	return string(data)
}

func (s *TaskState) FromJSON(data string) error {
	return json.Unmarshal([]byte(data), s)
}

func (s *TaskState) GenerateCheckPointID() string {
	if s.CheckPointID == "" {
		s.CheckPointID = fmt.Sprintf("task-%s-%d", s.MessageID, time.Now().UnixNano())
	}
	return s.CheckPointID
}

func (s *TaskState) CreateCheckpoint(stepID string) {
	s.LastCheckPointStep = stepID
	s.UpdatedAt = time.Now()
}

func (s *TaskState) CanResume() bool {
	return s.Status == TaskStatusRunning && s.LastCheckPointStep != ""
}
