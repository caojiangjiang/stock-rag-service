package agent

import (
	"context"
	"fmt"
	"strings"
)

// AnalystAgent 分析/写作 Agent
type AnalystAgent struct {
	name string
}

func NewAnalystAgent() *AnalystAgent {
	return &AnalystAgent{
		name: "analyst_agent",
	}
}

func (a *AnalystAgent) Name() string {
	return a.name
}

func (a *AnalystAgent) Execute(ctx context.Context, input *SpecialistRequest) (*SpecialistResponse, error) {
	if input.EvidenceSet == nil || len(input.EvidenceSet.Items) == 0 {
		return &SpecialistResponse{
			Success:    false,
			Error:      "缺少证据数据",
			Confidence: 0.2,
		}, nil
	}

	// 构建回答
	summary := a.buildAnswer(input)

	return &SpecialistResponse{
		Success:    true,
		Summary:    summary,
		Confidence: a.calculateConfidence(input),
	}, nil
}

func (a *AnalystAgent) buildAnswer(input *SpecialistRequest) string {
	var builder strings.Builder

	// 1. 问题重述
	builder.WriteString(fmt.Sprintf("**问题**: %s\n\n", input.UserMessage))

	// 2. 证据概述
	if input.EvidenceSet != nil && len(input.EvidenceSet.Items) > 0 {
		builder.WriteString("**参考文档**:\n")
		for i, item := range input.EvidenceSet.Items {
			if i >= 5 { // 最多显示5条
				builder.WriteString(fmt.Sprintf("... 还有 %d 条参考文档\n", len(input.EvidenceSet.Items)-5))
				break
			}
			builder.WriteString(fmt.Sprintf("[%d] %s (置信度: %.2f)\n",
				i+1, item.Title, item.Confidence))
		}
		builder.WriteString("\n")
	}

	// 3. 指标数据（如果有）
	if input.MetricTable != nil && len(input.MetricTable.Metrics) > 0 {
		builder.WriteString("**核心财务指标**:\n")
		builder.WriteString("| 指标 | 数值(亿元) | 年份 | 口径 |\n")
		builder.WriteString("|------|------------|------|------|\n")

		metricsByYear := make(map[string][]MetricItem)
		for _, metric := range input.MetricTable.Metrics {
			year := metric.Year
			if year == "" {
				year = "未知"
			}
			metricsByYear[year] = append(metricsByYear[year], metric)
		}

		for year, metrics := range metricsByYear {
			for _, metric := range metrics {
				builder.WriteString(fmt.Sprintf("| %s | %.2f | %s | %s |\n",
					metric.Name, metric.Value, year, metric.Caliber))
			}
		}
		builder.WriteString("\n")
	}

	// 4. 分析结论
	builder.WriteString(a.generateAnalysis(input))

	// 5. 风险提示
	builder.WriteString("\n**风险提示**:\n")
	builder.WriteString("- 以上分析基于公开文档信息，仅供参考\n")
	builder.WriteString("- 数据可能存在延迟或遗漏，请以官方公告为准\n")
	builder.WriteString("- 投资有风险，决策需谨慎\n")

	// 6. 引用来源
	if input.EvidenceSet != nil && len(input.EvidenceSet.Items) > 0 {
		builder.WriteString("\n**引用来源**:\n")
		for i, item := range input.EvidenceSet.Items[:min(3, len(input.EvidenceSet.Items))] {
			builder.WriteString(fmt.Sprintf("[%d] %s\n", i+1, item.Title))
		}
	}

	return builder.String()
}

func (a *AnalystAgent) generateAnalysis(input *SpecialistRequest) string {
	if input.MetricTable != nil && len(input.MetricTable.Metrics) > 0 {
		return a.analyzeWithMetrics(input)
	}
	return a.analyzeWithEvidence(input)
}

func (a *AnalystAgent) analyzeWithMetrics(input *SpecialistRequest) string {
	var builder strings.Builder
	builder.WriteString("**分析结论**:\n")

	// 简单的财务分析逻辑
	netProfit := findMetric(input.MetricTable, "归属于母公司所有者的净利润")
	revenue := findMetric(input.MetricTable, "营业收入")

	if netProfit != nil && revenue != nil {
		margin := (netProfit.Value / revenue.Value) * 100
		builder.WriteString(fmt.Sprintf("- %s年实现营业收入%.2f亿元，归属于母公司所有者的净利润%.2f亿元\n",
			netProfit.Year, revenue.Value, netProfit.Value))
		builder.WriteString(fmt.Sprintf("- 净利润率为%.2f%%\n", margin))
	}

	// 同比分析
	prevYearNetProfit := findMetricByYear(input.MetricTable, getPrevYear(netProfit))
	if netProfit != nil && prevYearNetProfit != nil {
		growth := ((netProfit.Value - prevYearNetProfit.Value) / prevYearNetProfit.Value) * 100
		builder.WriteString(fmt.Sprintf("- 净利润同比增长%.2f%%\n", growth))
	}

	return builder.String()
}

func (a *AnalystAgent) analyzeWithEvidence(input *SpecialistRequest) string {
	var builder strings.Builder
	builder.WriteString("**分析结论**:\n")

	var keyPoints []string
	if input.EvidenceSet != nil {
		// 从证据中提取关键信息
		keyPoints = a.extractKeyPoints(input.EvidenceSet)
		for _, point := range keyPoints {
			builder.WriteString(fmt.Sprintf("- %s\n", point))
		}
	}

	if len(keyPoints) == 0 {
		builder.WriteString("- 根据现有参考文档，未能提取到直接相关的分析信息\n")
	}

	return builder.String()
}

func (a *AnalystAgent) extractKeyPoints(evidenceSet *EvidenceSet) []string {
	var points []string

	for _, item := range evidenceSet.Items {
		if item.Confidence >= 0.6 && len(points) < 5 {
			// 提取内容摘要（前150字符）
			content := item.Content
			if len(content) > 150 {
				content = content[:150] + "..."
			}
			points = append(points, content)
		}
	}

	return points
}

func (a *AnalystAgent) calculateConfidence(input *SpecialistRequest) float64 {
	confidence := 0.5

	// 基于证据质量
	if input.EvidenceSet != nil {
		highQualityCount := 0
		for _, item := range input.EvidenceSet.Items {
			if item.Quality == "high" {
				highQualityCount++
			}
		}
		confidence += float64(highQualityCount) * 0.1
	}

	// 基于指标完整性
	if input.MetricTable != nil && len(input.MetricTable.Metrics) > 0 {
		confidence += 0.2
	}

	if confidence > 0.9 {
		confidence = 0.9
	}

	return confidence
}

func findMetric(table *MetricTable, name string) *MetricItem {
	for _, m := range table.Metrics {
		if m.Name == name {
			return &m
		}
	}
	return nil
}

func findMetricByYear(table *MetricTable, year string) *MetricItem {
	if year == "" {
		return nil
	}
	for _, m := range table.Metrics {
		if m.Year == year && strings.Contains(m.Name, "净利润") {
			return &m
		}
	}
	return nil
}

func getPrevYear(metric *MetricItem) string {
	if metric == nil || metric.Year == "" {
		return ""
	}
	year, _ := fmt.Sscanf(metric.Year, "%d", new(int))
	return fmt.Sprintf("%d", year-1)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
