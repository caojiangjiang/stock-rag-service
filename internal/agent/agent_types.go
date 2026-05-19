package agent

import (
	"context"

	"stock_rag/internal/model"
	"stock_rag/internal/router"
)

type EvidenceItem = model.EvidenceItem
type EvidenceSet = model.EvidenceSet
type MetricItem = model.MetricItem
type MetricTable = model.MetricTable

type ExecutionPlan struct {
	Steps       []PlanStep `json:"steps"`
	Requires    []string   `json:"requires"`
	IsComplex   bool       `json:"is_complex"`
	Description string     `json:"description"`
}

type PlanStep struct {
	Step     int    `json:"step"`
	Agent    string `json:"agent"`
	Task     string `json:"task"`
	Input    string `json:"input"`
	Expected string `json:"expected"`
}

type SpecialistAgent interface {
	Name() string
	Execute(ctx context.Context, input *SpecialistRequest) (*SpecialistResponse, error)
}

type SpecialistRequest struct {
	UserMessage  string       `json:"user_message"`
	StockCode    string       `json:"stock_code"`
	EvidenceSet  *EvidenceSet `json:"evidence_set,omitempty"`
	MetricTable  *MetricTable `json:"metric_table,omitempty"`
	Plan         *ExecutionPlan `json:"plan,omitempty"`
}

type SpecialistResponse struct {
	Success     bool          `json:"success"`
	EvidenceSet *EvidenceSet  `json:"evidence_set,omitempty"`
	MetricTable *MetricTable  `json:"metric_table,omitempty"`
	Summary     string        `json:"summary,omitempty"`
	Confidence  float64       `json:"confidence"`
	Error       string        `json:"error,omitempty"`
}

type SupervisorAgent interface {
	Name() string
	Mode() router.RouteMode
	Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error)
}