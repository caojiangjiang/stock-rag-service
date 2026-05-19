package model

import (
	"encoding/json"
	"time"
)

type EvidenceItem struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Content       string    `json:"content"`
	DocType       string    `json:"doc_type"`
	SourceURL     string    `json:"source_url"`
	Published     time.Time `json:"published"`
	PageNo        int       `json:"page_no"`
	Confidence    float64   `json:"confidence"`
	WhySelected   string    `json:"why_selected"`
	Quality       string    `json:"quality"`
	StockCode     string    `json:"stock_code"`
}

type EvidenceSet struct {
	Query      string        `json:"query"`
	TotalCount int           `json:"total_count"`
	Items      []EvidenceItem `json:"items"`
}

func (e *EvidenceSet) ToJSON() string {
	data, _ := json.Marshal(e)
	return string(data)
}

type MetricItem struct {
	Name    string  `json:"name"`
	Value   float64 `json:"value"`
	Unit    string  `json:"unit"`
	Year    string  `json:"year"`
	Source  string  `json:"source"`
	Caliber string  `json:"caliber"`
}

type MetricTable struct {
	StockCode string      `json:"stock_code"`
	Metrics   []MetricItem `json:"metrics"`
}

type SpecialistRequest struct {
	UserMessage  string       `json:"user_message"`
	StockCode    string       `json:"stock_code"`
	EvidenceSet  *EvidenceSet `json:"evidence_set"`
	MetricTable  *MetricTable `json:"metric_table"`
}

type SpecialistResponse struct {
	Success     bool          `json:"success"`
	Error       string        `json:"error"`
	Summary     string        `json:"summary"`
	Confidence  float64       `json:"confidence"`
	EvidenceSet *EvidenceSet  `json:"evidence_set"`
	MetricTable *MetricTable  `json:"metric_table"`
}
