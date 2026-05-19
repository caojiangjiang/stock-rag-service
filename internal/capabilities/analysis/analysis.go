package analysis

import (
	"fmt"
	"strings"

	"stock_rag/internal/capabilities/metrics"
)

// ReportGenerator 报告生成器接口
type ReportGenerator interface {
	GenerateSummary(evidence []string, metrics []metrics.FinancialMetric) string
	GenerateComparison(report1, report2 string) string
}

// SimpleReportGenerator 简单报告生成器
type SimpleReportGenerator struct{}

// NewSimpleReportGenerator 创建报告生成器
func NewSimpleReportGenerator() *SimpleReportGenerator {
	return &SimpleReportGenerator{}
}

// GenerateSummary 生成总结报告
func (g *SimpleReportGenerator) GenerateSummary(evidence []string, metrics []metrics.FinancialMetric) string {
	var sb strings.Builder

	sb.WriteString("【分析报告】\n\n")

	if len(metrics) > 0 {
		sb.WriteString("一、财务指标摘要\n")
		for _, m := range metrics {
			sb.WriteString(fmt.Sprintf("- %s: %.2f %s\n", m.Name, m.Value, m.Unit))
		}
		sb.WriteString("\n")
	}

	if len(evidence) > 0 {
		sb.WriteString("二、关键证据\n")
		for i, ev := range evidence {
			preview := ev
			if len(preview) > 150 {
				preview = preview[:150] + "..."
			}
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, preview))
		}
	}

	return sb.String()
}

// GenerateComparison 生成对比报告
func (g *SimpleReportGenerator) GenerateComparison(report1, report2 string) string {
	return fmt.Sprintf("【对比分析】\n\n报告一：\n%s\n\n报告二：\n%s", report1, report2)
}

// AnalysisResult 分析结果
type AnalysisResult struct {
	Content    string                  `json:"content"`
	Evidence   []string                `json:"evidence"`
	Metrics    []metrics.FinancialMetric `json:"metrics"`
	Confidence float64                 `json:"confidence"`
}
