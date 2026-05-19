package metrics

import (
	"regexp"
	"strconv"
	"strings"
)

// FinancialMetric 财务指标
type FinancialMetric struct {
	Name      string  `json:"name"`
	Value     float64 `json:"value"`
	Unit      string  `json:"unit"`
	Year      string  `json:"year"`
	Source    string  `json:"source"`
	Confidence float64 `json:"confidence"`
}

// MetricExtractor 指标提取器接口
type MetricExtractor interface {
	Extract(text string, metricNames []string) []FinancialMetric
}

// SimpleMetricExtractor 简单指标提取器
type SimpleMetricExtractor struct {
	metricPatterns map[string]*regexp.Regexp
}

// NewSimpleMetricExtractor 创建指标提取器
func NewSimpleMetricExtractor() *SimpleMetricExtractor {
	return &SimpleMetricExtractor{
		metricPatterns: buildMetricPatterns(),
	}
}

func buildMetricPatterns() map[string]*regexp.Regexp {
	return map[string]*regexp.Regexp{
		"营业收入":    regexp.MustCompile(`营业收入[\s：:]*([\d,.]+)\s*(亿元|万元|元)?`),
		"净利润":     regexp.MustCompile(`净利润[\s：:]*([\d,.]+)\s*(亿元|万元|元)?`),
		"归母净利润":  regexp.MustCompile(`归属于母公司所有者的净利润[\s：:]*([\d,.]+)\s*(亿元|万元|元)?`),
		"总资产":     regexp.MustCompile(`总资产[\s：:]*([\d,.]+)\s*(亿元|万元|元)?`),
		"净资产":     regexp.MustCompile(`净资产[\s：:]*([\d,.]+)\s*(亿元|万元|元)?`),
		"净资产收益率": regexp.MustCompile(`净资产收益率[\s：:]*([\d,.]+)%?`),
		"毛利率":     regexp.MustCompile(`毛利率[\s：:]*([\d,.]+)%?`),
		"净利率":     regexp.MustCompile(`净利率[\s：:]*([\d,.]+)%?`),
		"研发投入":    regexp.MustCompile(`研发投入[\s：:]*([\d,.]+)\s*(亿元|万元)?`),
	}
}

// Extract 从文本中提取指标
func (e *SimpleMetricExtractor) Extract(text string, metricNames []string) []FinancialMetric {
	var results []FinancialMetric

	for _, name := range metricNames {
		if pattern, ok := e.metricPatterns[name]; ok {
			matches := pattern.FindStringSubmatch(text)
			if len(matches) >= 2 {
				value, err := parseNumber(matches[1])
				if err == nil {
					unit := ""
					if len(matches) >= 3 {
						unit = matches[2]
					}
					results = append(results, FinancialMetric{
						Name:      name,
						Value:     value,
						Unit:      unit,
						Confidence: 0.8,
					})
				}
			}
		}
	}

	return results
}

func parseNumber(s string) (float64, error) {
	s = strings.ReplaceAll(s, ",", "")
	return strconv.ParseFloat(s, 64)
}

// MetricAlignment 指标对齐结果
type MetricAlignment struct {
	MetricName string            `json:"metric_name"`
	YearValues map[string]float64 `json:"year_values"`
	Unit       string            `json:"unit"`
}

// AlignMetrics 对齐不同年份的指标
func AlignMetrics(metrics []FinancialMetric) []MetricAlignment {
	alignmentMap := make(map[string]*MetricAlignment)

	for _, m := range metrics {
		if _, ok := alignmentMap[m.Name]; !ok {
			alignmentMap[m.Name] = &MetricAlignment{
				MetricName: m.Name,
				YearValues: make(map[string]float64),
				Unit:       m.Unit,
			}
		}
		if m.Year != "" {
			alignmentMap[m.Name].YearValues[m.Year] = m.Value
		}
	}

	var results []MetricAlignment
	for _, v := range alignmentMap {
		results = append(results, *v)
	}

	return results
}
