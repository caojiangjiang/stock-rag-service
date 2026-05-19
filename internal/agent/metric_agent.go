package agent

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

// MetricAgent 财务指标提取 Agent
type MetricAgent struct {
	name string
}

func NewMetricAgent() *MetricAgent {
	return &MetricAgent{
		name: "metric_agent",
	}
}

func (a *MetricAgent) Name() string {
	return a.name
}

func (a *MetricAgent) Execute(ctx context.Context, input *SpecialistRequest) (*SpecialistResponse, error) {
	if input.EvidenceSet == nil || len(input.EvidenceSet.Items) == 0 {
		return &SpecialistResponse{
			Success:    false,
			Error:      "没有可用的证据数据",
			Confidence: 0,
		}, nil
	}

	metricTable := &MetricTable{
		StockCode: input.StockCode,
		Metrics:   make([]MetricItem, 0),
	}

	for _, evidence := range input.EvidenceSet.Items {
		if evidence.Quality == "low" {
			continue
		}

		metrics := extractMetricsFromContent(evidence.Content, evidence.Title, evidence.DocType)
		for _, metric := range metrics {
			metric.Source = evidence.Title
			metricTable.Metrics = append(metricTable.Metrics, metric)
		}
	}

	// 做年份对齐和口径统一
	metricTable.Metrics = alignYears(metricTable.Metrics)
	metricTable.Metrics = unifyCaliber(metricTable.Metrics)

	return &SpecialistResponse{
		Success:     true,
		MetricTable: metricTable,
		Confidence:  calculateMetricConfidence(metricTable),
	}, nil
}

// 从内容中提取财务指标
func extractMetricsFromContent(content, title, docType string) []MetricItem {
	var metrics []MetricItem

	// 匹配常见财务指标模式
	patterns := map[string]string{
		"营业收入":    `(营业收入|营收)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"净利润":     `(净利润|归属于母公司所有者的净利润|归母净利润)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"扣非净利润":   `(扣除非经常性损益的净利润|扣非净利润)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"经营活动现金流": `(经营活动产生的现金流量净额|经营现金流)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"总资产":     `(总资产)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"净资产":     `(净资产|所有者权益)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"毛利率":     `(毛利率)(?:[\s：:])([\d.]+)%`,
		"净利率":     `(净利率)(?:[\s：:])([\d.]+)%`,
		"ROE":     `ROE(?:[\s：:])([\d.]+)%`,
	}

	year := extractYearFromTitle(title)

	for metricName, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllStringSubmatch(content, -1)

		for _, match := range matches {
			if len(match) >= 3 {
				value, _ := strconv.ParseFloat(strings.ReplaceAll(match[2], ",", ""), 64)
				unit := "元"
				if len(match) >= 4 {
					unit = match[3]
				}

				// 单位转换为亿元
				value = convertToYuan(value, unit)

				metrics = append(metrics, MetricItem{
					Name:    metricName,
					Value:   value,
					Unit:    "亿元",
					Year:    year,
					Caliber: determineCaliber(metricName, docType),
				})
			}
		}
	}

	return metrics
}

func extractYearFromTitle(title string) string {
	re := regexp.MustCompile(`(\d{4})年`)
	match := re.FindStringSubmatch(title)
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

func convertToYuan(value float64, unit string) float64 {
	switch unit {
	case "万元":
		return value / 10000
	case "亿元":
		return value
	default: // 元
		return value / 100000000
	}
}

func determineCaliber(metricName, docType string) string {
	// 判断指标口径
	switch metricName {
	case "净利润":
		if strings.Contains(docType, "合并") {
			return "合并报表"
		}
		return "归属于母公司所有者"
	case "扣非净利润":
		return "扣除非经常性损益"
	default:
		return "合并报表"
	}
}

func alignYears(metrics []MetricItem) []MetricItem {
	// 简单的年份对齐逻辑
	// 实际应用中可能需要更复杂的逻辑
	return metrics
}

func unifyCaliber(metrics []MetricItem) []MetricItem {
	// 统一指标口径
	for i, m := range metrics {
		// 将净利润统一为归母净利润
		if m.Name == "净利润" && m.Caliber == "合并报表" {
			metrics[i].Name = "归属于母公司所有者的净利润"
			metrics[i].Caliber = "归属于母公司所有者"
		}
	}
	return metrics
}

func calculateMetricConfidence(table *MetricTable) float64 {
	if len(table.Metrics) == 0 {
		return 0.3
	}

	// 基于指标数量和质量计算置信度
	baseConfidence := 0.5 + float64(len(table.Metrics))*0.1
	if baseConfidence > 0.9 {
		baseConfidence = 0.9
	}
	return baseConfidence
}
