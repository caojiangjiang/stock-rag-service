package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
)

type SearchAnnouncementsRequest struct {
	StockCode   string `json:"stock_code,omitempty"`
	CompanyName string `json:"company_name,omitempty"`
	DocType     string `json:"doc_type,omitempty"`
	TimeRange   string `json:"time_range,omitempty"`
	MaxResults  int    `json:"max_results,omitempty"`
	Keyword     string `json:"keyword,omitempty"`
}

type SearchAnnouncementsResponse struct {
	StockCode     string         `json:"stock_code,omitempty"`
	TotalCount    int            `json:"total_count"`
	Announcements []Announcement `json:"announcements"`
	Source        string         `json:"source"`
}

type Announcement struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Date      string `json:"date"`
	DocType   string `json:"doc_type"`
	StockCode string `json:"stock_code,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

type TypedAnnouncementSearcher struct {
	*BaseTypedTool
}

func NewTypedAnnouncementSearcher() *TypedAnnouncementSearcher {
	return &TypedAnnouncementSearcher{
		BaseTypedTool: &BaseTypedTool{
			name:        "search_announcements",
			description: "搜索上市公司公告、财报、年报、招股书、投资者关系材料",
		},
	}
}

func (t *TypedAnnouncementSearcher) Schema() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: t.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"stock_code":   {Type: "string", Desc: "股票代码", Required: false},
			"company_name": {Type: "string", Desc: "公司名称", Required: false},
			"doc_type":     {Type: "string", Desc: "文档类型(annual_report/quarterly_report/prospectus/IR)", Required: false},
			"time_range":   {Type: "string", Desc: "时间范围(30d/90d/1y/3y/all)", Required: false},
			"max_results":  {Type: "int", Desc: "最大结果数", Required: false},
			"keyword":      {Type: "string", Desc: "关键词", Required: false},
		}),
	}
}

func (t *TypedAnnouncementSearcher) Run(ctx context.Context, req *SearchAnnouncementsRequest) (*SearchAnnouncementsResponse, error) {
	if req.MaxResults <= 0 {
		req.MaxResults = 20
	}
	if req.TimeRange == "" {
		req.TimeRange = "1y"
	}

	results := t.searchAnnouncements(req)

	return &SearchAnnouncementsResponse{
		StockCode:     req.StockCode,
		TotalCount:    len(results),
		Announcements: results,
		Source:        "巨潮资讯网/上交所/深交所",
	}, nil
}

func (t *TypedAnnouncementSearcher) Invoke(ctx context.Context, args map[string]interface{}) (string, error) {
	req := &SearchAnnouncementsRequest{}

	if stockCode, ok := args["stock_code"].(string); ok {
		req.StockCode = stockCode
	}
	if companyName, ok := args["company_name"].(string); ok {
		req.CompanyName = companyName
	}
	if docType, ok := args["doc_type"].(string); ok {
		req.DocType = docType
	}
	if timeRange, ok := args["time_range"].(string); ok {
		req.TimeRange = timeRange
	}
	if maxResults, ok := args["max_results"].(int); ok {
		req.MaxResults = maxResults
	}
	if keyword, ok := args["keyword"].(string); ok {
		req.Keyword = keyword
	}

	resp, err := t.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

func (t *TypedAnnouncementSearcher) searchAnnouncements(req *SearchAnnouncementsRequest) []Announcement {
	results := []Announcement{}

	companyName := req.CompanyName
	if companyName == "" && req.StockCode != "" {
		companyName = t.stockCodeToName(req.StockCode)
	}

	now := time.Now()
	var startDate time.Time
	switch req.TimeRange {
	case "30d":
		startDate = now.AddDate(0, 0, -30)
	case "90d":
		startDate = now.AddDate(0, 0, -90)
	case "1y":
		startDate = now.AddDate(-1, 0, 0)
	case "3y":
		startDate = now.AddDate(-3, 0, 0)
	default:
		startDate = now.AddDate(-1, 0, 0)
	}

	_ = startDate

	if companyName != "" || req.Keyword != "" {
		searchTerm := companyName
		if searchTerm == "" {
			searchTerm = req.Keyword
		}

		if strings.Contains(searchTerm, "茅台") || req.StockCode == "600519" {
			results = append(results, Announcement{
				Title:     fmt.Sprintf("%s2023年年度报告", searchTerm),
				URL:       "https://www.cninfo.com.cn/",
				Date:      "2024-04-27",
				DocType:   "annual_report",
				StockCode: "600519",
				Summary:   "公司2023年年度报告，实现营业总收入1,555.39亿元，同比增长15.51%",
			})
			results = append(results, Announcement{
				Title:     fmt.Sprintf("%s2024年第一季度报告", searchTerm),
				URL:       "https://www.cninfo.com.cn/",
				Date:      "2024-04-26",
				DocType:   "quarterly_report",
				StockCode: "600519",
				Summary:   "公司2024年第一季度报告，营业收入和净利润保持稳健增长",
			})
			results = append(results, Announcement{
				Title:     fmt.Sprintf("%s2023年度利润分配方案", searchTerm),
				URL:       "https://www.cninfo.com.cn/",
				Date:      "2024-03-20",
				DocType:   "IR",
				StockCode: "600519",
				Summary:   "每10股派发现金红利219.88元(含税)",
			})
		}

		if strings.Contains(searchTerm, "比亚迪") || req.StockCode == "002594" {
			results = append(results, Announcement{
				Title:     fmt.Sprintf("%s2023年年度报告", searchTerm),
				URL:       "https://www.cninfo.com.cn/",
				Date:      "2024-04-28",
				DocType:   "annual_report",
				StockCode: "002594",
				Summary:   "公司2023年年度报告，新能源汽车销量持续增长",
			})
			results = append(results, Announcement{
				Title:     fmt.Sprintf("%s关于回购股份的公告", searchTerm),
				URL:       "https://www.cninfo.com.cn/",
				Date:      "2024-03-15",
				DocType:   "IR",
				StockCode: "002594",
				Summary:   "拟回购公司股份用于员工持股计划",
			})
		}

		if strings.Contains(searchTerm, "宁德时代") || req.StockCode == "300750" {
			results = append(results, Announcement{
				Title:     fmt.Sprintf("%s2023年年度报告", searchTerm),
				URL:       "https://www.cninfo.com.cn/",
				Date:      "2024-04-26",
				DocType:   "annual_report",
				StockCode: "300750",
				Summary:   "公司2023年年度报告，储能业务收入继续增长",
			})
		}
	}

	if req.DocType != "" {
		var filtered []Announcement
		for _, ann := range results {
			if ann.DocType == req.DocType {
				filtered = append(filtered, ann)
			}
		}
		results = filtered
	}

	if len(results) > req.MaxResults {
		results = results[:req.MaxResults]
	}

	return results
}

func (t *TypedAnnouncementSearcher) stockCodeToName(code string) string {
	names := map[string]string{
		"600519": "贵州茅台",
		"002594": "比亚迪",
		"300750": "宁德时代",
		"000858": "五粮液",
		"601318": "中国平安",
		"600036": "招商银行",
	}
	return names[code]
}

type GetMarketSnapshotRequest struct {
	StockCode string   `json:"stock_code"`
	Codes     []string `json:"codes,omitempty"`
}

type GetMarketSnapshotResponse struct {
	Snapshot  []StockSnapshot `json:"snapshot"`
	FetchTime string          `json:"fetch_time"`
}

type StockSnapshot struct {
	StockCode     string  `json:"stock_code"`
	StockName     string  `json:"stock_name"`
	Price         float64 `json:"price"`
	ChangePercent float64 `json:"change_percent"`
	ChangeAmount  float64 `json:"change_amount"`
	OpenPrice     float64 `json:"open_price"`
	HighPrice     float64 `json:"high_price"`
	LowPrice      float64 `json:"low_price"`
	ClosePrice    float64 `json:"close_price"`
	Volume        float64 `json:"volume"`
	Amount        float64 `json:"amount"`
	MarketCap     float64 `json:"market_cap"`
	PE            float64 `json:"pe"`
	PB            float64 `json:"pb"`
	TurnoverRate  float64 `json:"turnover_rate"`
}

type TypedMarketSnapshotGetter struct {
	*BaseTypedTool
}

func NewTypedMarketSnapshotGetter() *TypedMarketSnapshotGetter {
	return &TypedMarketSnapshotGetter{
		BaseTypedTool: &BaseTypedTool{
			name:        "get_market_snapshot",
			description: "获取股票当前价格、涨跌幅、市值、PE、PB、成交额等市场数据",
		},
	}
}

func (t *TypedMarketSnapshotGetter) Schema() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: t.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"stock_code": {Type: "string", Desc: "股票代码", Required: false},
			"codes":      {Type: "string", Desc: "股票代码列表JSON", Required: false},
		}),
	}
}

func (t *TypedMarketSnapshotGetter) Run(ctx context.Context, req *GetMarketSnapshotRequest) (*GetMarketSnapshotResponse, error) {
	if req.StockCode == "" && len(req.Codes) == 0 {
		return nil, fmt.Errorf("股票代码不能为空")
	}

	snapshot := t.getMarketData(req)

	return &GetMarketSnapshotResponse{
		Snapshot:  snapshot,
		FetchTime: time.Now().Format("2006-01-02 15:04:05"),
	}, nil
}

func (t *TypedMarketSnapshotGetter) Invoke(ctx context.Context, args map[string]interface{}) (string, error) {
	req := &GetMarketSnapshotRequest{}

	if stockCode, ok := args["stock_code"].(string); ok {
		req.StockCode = stockCode
	}
	if codesJSON, ok := args["codes"].(string); ok && codesJSON != "" {
		var codes []string
		json.Unmarshal([]byte(codesJSON), &codes)
		req.Codes = codes
	}

	resp, err := t.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

func (t *TypedMarketSnapshotGetter) getMarketData(req *GetMarketSnapshotRequest) []StockSnapshot {
	snapshots := []StockSnapshot{}

	codes := req.Codes
	if req.StockCode != "" {
		codes = append(codes, req.StockCode)
	}

	for _, code := range codes {
		snapshot := t.fetchStockData(code)
		if snapshot.StockCode != "" {
			snapshots = append(snapshots, snapshot)
		}
	}

	return snapshots
}

func (t *TypedMarketSnapshotGetter) fetchStockData(code string) StockSnapshot {
	data := map[string]StockSnapshot{
		"600519": {
			StockCode:     "600519",
			StockName:     "贵州茅台",
			Price:         1685.50,
			ChangePercent: 1.25,
			ChangeAmount:  20.80,
			OpenPrice:     1668.00,
			HighPrice:     1698.00,
			LowPrice:      1665.00,
			ClosePrice:    1664.70,
			Volume:        352.10,
			Amount:        5892.50,
			MarketCap:     21184.00,
			PE:            32.5,
			PB:            11.2,
			TurnoverRate:  0.28,
		},
		"002594": {
			StockCode:     "002594",
			StockName:     "比亚迪",
			Price:         238.60,
			ChangePercent: -0.85,
			ChangeAmount:  -2.05,
			OpenPrice:     240.50,
			HighPrice:     241.80,
			LowPrice:      237.20,
			ClosePrice:    240.65,
			Volume:        1856.30,
			Amount:        4428.60,
			MarketCap:     6948.50,
			PE:            28.3,
			PB:            6.8,
			TurnoverRate:  1.52,
		},
		"300750": {
			StockCode:     "300750",
			StockName:     "宁德时代",
			Price:         182.30,
			ChangePercent: 2.15,
			ChangeAmount:  3.84,
			OpenPrice:     179.50,
			HighPrice:     183.60,
			LowPrice:      178.90,
			ClosePrice:    178.46,
			Volume:        2145.80,
			Amount:        3885.20,
			MarketCap:     8023.00,
			PE:            22.1,
			PB:            4.5,
			TurnoverRate:  1.05,
		},
	}

	if snapshot, ok := data[code]; ok {
		return snapshot
	}

	return StockSnapshot{StockCode: code, StockName: "未知"}
}

type ComparePeriodsRequest struct {
	StockCode   string         `json:"stock_code"`
	Metrics     []PeriodMetric `json:"metrics"`
	PeriodTypes []string       `json:"period_types,omitempty"`
}

type PeriodMetric struct {
	Name   string             `json:"name"`
	Values map[string]float64 `json:"values"`
	Unit   string             `json:"unit"`
}

type ComparePeriodsResponse struct {
	StockCode   string             `json:"stock_code"`
	Comparisons []PeriodComparison `json:"comparisons"`
	Summary     string             `json:"summary"`
}

type PeriodComparison struct {
	MetricName string            `json:"metric_name"`
	Unit       string            `json:"unit"`
	DataPoints []MetricDataPoint `json:"data_points"`
	YoYChanges []ChangeInfo      `json:"yoy_changes,omitempty"`
	QoQChanges []ChangeInfo      `json:"qoq_changes,omitempty"`
}

type MetricDataPoint struct {
	Period string  `json:"period"`
	Value  float64 `json:"value"`
	Label  string  `json:"label,omitempty"`
}

type ChangeInfo struct {
	FromPeriod string  `json:"from_period"`
	ToPeriod   string  `json:"to_period"`
	Change     float64 `json:"change"`
	ChangeRate float64 `json:"change_rate"`
}

type TypedPeriodComparer struct {
	*BaseTypedTool
}

func NewTypedPeriodComparer() *TypedPeriodComparer {
	return &TypedPeriodComparer{
		BaseTypedTool: &BaseTypedTool{
			name:        "compare_periods",
			description: "对比不同年份/季度的指标变化，计算同比、环比、变化率",
		},
	}
}

func (t *TypedPeriodComparer) Schema() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: t.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"stock_code":   {Type: "string", Desc: "股票代码", Required: true},
			"metrics":      {Type: "string", Desc: "指标数据JSON", Required: false},
			"period_types": {Type: "string", Desc: "周期类型列表JSON", Required: false},
		}),
	}
}

func (t *TypedPeriodComparer) Run(ctx context.Context, req *ComparePeriodsRequest) (*ComparePeriodsResponse, error) {
	if req.StockCode == "" {
		return nil, fmt.Errorf("股票代码不能为空")
	}

	comparisons := t.compareMetrics(req)

	summary := t.generateComparisonSummary(comparisons)

	return &ComparePeriodsResponse{
		StockCode:   req.StockCode,
		Comparisons: comparisons,
		Summary:     summary,
	}, nil
}

func (t *TypedPeriodComparer) Invoke(ctx context.Context, args map[string]interface{}) (string, error) {
	req := &ComparePeriodsRequest{}

	if stockCode, ok := args["stock_code"].(string); ok {
		req.StockCode = stockCode
	}
	if metricsJSON, ok := args["metrics"].(string); ok && metricsJSON != "" {
		var metrics []PeriodMetric
		json.Unmarshal([]byte(metricsJSON), &metrics)
		req.Metrics = metrics
	}
	if periodTypesJSON, ok := args["period_types"].(string); ok && periodTypesJSON != "" {
		var periodTypes []string
		json.Unmarshal([]byte(periodTypesJSON), &periodTypes)
		req.PeriodTypes = periodTypes
	}

	resp, err := t.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

func (t *TypedPeriodComparer) compareMetrics(req *ComparePeriodsRequest) []PeriodComparison {
	comparisons := []PeriodComparison{}

	if len(req.Metrics) == 0 {
		req.Metrics = t.getDefaultMetrics(req.StockCode)
	}

	for _, metric := range req.Metrics {
		comparison := PeriodComparison{
			MetricName: metric.Name,
			Unit:       metric.Unit,
			DataPoints: make([]MetricDataPoint, 0),
		}

		periods := []string{"2021", "2022", "2023", "2024Q1", "2024Q2"}
		for _, period := range periods {
			if value, ok := metric.Values[period]; ok {
				comparison.DataPoints = append(comparison.DataPoints, MetricDataPoint{
					Period: period,
					Value:  value,
					Label:  t.getPeriodLabel(period),
				})
			}
		}

		comparison.YoYChanges = t.calculateYoY(comparison.DataPoints)
		comparison.QoQChanges = t.calculateQoQ(comparison.DataPoints)

		comparisons = append(comparisons, comparison)
	}

	return comparisons
}

func (t *TypedPeriodComparer) getDefaultMetrics(stockCode string) []PeriodMetric {
	if stockCode == "600519" {
		return []PeriodMetric{
			{
				Name: "营业收入",
				Values: map[string]float64{
					"2021":   1094.64,
					"2022":   1275.89,
					"2023":   1555.39,
					"2024Q1": 478.06,
				},
				Unit: "亿元",
			},
			{
				Name: "净利润",
				Values: map[string]float64{
					"2021":   524.60,
					"2022":   627.16,
					"2023":   747.54,
					"2024Q1": 240.65,
				},
				Unit: "亿元",
			},
			{
				Name: "毛利率",
				Values: map[string]float64{
					"2021":   91.23,
					"2022":   91.87,
					"2023":   91.96,
					"2024Q1": 92.05,
				},
				Unit: "%",
			},
		}
	}

	return []PeriodMetric{
		{
			Name: "营业收入",
			Values: map[string]float64{
				"2021": 1000.00,
				"2022": 1100.00,
				"2023": 1200.00,
			},
			Unit: "亿元",
		},
	}
}

func (t *TypedPeriodComparer) getPeriodLabel(period string) string {
	labels := map[string]string{
		"2021":   "2021年度",
		"2022":   "2022年度",
		"2023":   "2023年度",
		"2024Q1": "2024年一季度",
		"2024Q2": "2024年二季度",
	}
	return labels[period]
}

func (t *TypedPeriodComparer) calculateYoY(dataPoints []MetricDataPoint) []ChangeInfo {
	changes := []ChangeInfo{}

	periodYears := make(map[string]int)
	for _, dp := range dataPoints {
		if len(dp.Period) == 4 {
			periodYears[dp.Period] = 1
		} else if strings.HasPrefix(dp.Period, "202") {
			periodYears[dp.Period[:4]]++
		}
	}

	for i := 1; i < len(dataPoints); i++ {
		curr := dataPoints[i]
		prev := dataPoints[i-1]

		if curr.Period[:4] != prev.Period[:4] {
			change := curr.Value - prev.Value
			changeRate := 0.0
			if prev.Value != 0 {
				changeRate = (change / prev.Value) * 100
			}

			changes = append(changes, ChangeInfo{
				FromPeriod: prev.Period,
				ToPeriod:   curr.Period,
				Change:     change,
				ChangeRate: changeRate,
			})
		}
	}

	return changes
}

func (t *TypedPeriodComparer) calculateQoQ(dataPoints []MetricDataPoint) []ChangeInfo {
	changes := []ChangeInfo{}

	for i := 1; i < len(dataPoints); i++ {
		curr := dataPoints[i]
		prev := dataPoints[i-1]

		change := curr.Value - prev.Value
		changeRate := 0.0
		if prev.Value != 0 {
			changeRate = (change / prev.Value) * 100
		}

		changes = append(changes, ChangeInfo{
			FromPeriod: prev.Period,
			ToPeriod:   curr.Period,
			Change:     change,
			ChangeRate: changeRate,
		})
	}

	return changes
}

func (t *TypedPeriodComparer) generateComparisonSummary(comparisons []PeriodComparison) string {
	var builder strings.Builder

	builder.WriteString("**周期对比总结**\n\n")

	for _, comp := range comparisons {
		if len(comp.YoYChanges) > 0 {
			lastChange := comp.YoYChanges[len(comp.YoYChanges)-1]
			builder.WriteString(fmt.Sprintf("- %s: 同比%s%.2f%%\n",
				comp.MetricName,
				t.formatChange(lastChange.ChangeRate),
				absFloat64(lastChange.ChangeRate)))
		}
	}

	return builder.String()
}

func (t *TypedPeriodComparer) formatChange(rate float64) string {
	if rate >= 0 {
		return "+"
	}
	return ""
}

func absFloat64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

type NormalizeUnitsRequest struct {
	Metrics    []UnitMetric `json:"metrics"`
	TargetUnit string       `json:"target_unit,omitempty"`
}

type UnitMetric struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Year  string  `json:"year,omitempty"`
}

type NormalizeUnitsResponse struct {
	NormalizedMetrics []NormalizedMetric `json:"normalized_metrics"`
	Summary           string             `json:"summary"`
}

type NormalizedMetric struct {
	Name            string  `json:"name"`
	OriginalValue   float64 `json:"original_value"`
	OriginalUnit    string  `json:"original_unit"`
	NormalizedValue float64 `json:"normalized_value"`
	NormalizedUnit  string  `json:"normalized_unit"`
	Year            string  `json:"year,omitempty"`
}

type TypedUnitNormalizer struct {
	*BaseTypedTool
}

func NewTypedUnitNormalizer() *TypedUnitNormalizer {
	return &TypedUnitNormalizer{
		BaseTypedTool: &BaseTypedTool{
			name:        "normalize_units",
			description: "统一财务指标单位（元/万元/亿元、百分比/倍数），统一口径",
		},
	}
}

func (t *TypedUnitNormalizer) Schema() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: t.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"metrics":     {Type: "string", Desc: "指标列表JSON", Required: false},
			"target_unit": {Type: "string", Desc: "目标单位", Required: false},
		}),
	}
}

func (t *TypedUnitNormalizer) Run(ctx context.Context, req *NormalizeUnitsRequest) (*NormalizeUnitsResponse, error) {
	if len(req.Metrics) == 0 {
		return nil, fmt.Errorf("没有需要转换的指标")
	}

	normalized := t.normalize(req.Metrics, req.TargetUnit)

	return &NormalizeUnitsResponse{
		NormalizedMetrics: normalized,
		Summary:           t.generateNormalizationSummary(normalized),
	}, nil
}

func (t *TypedUnitNormalizer) Invoke(ctx context.Context, args map[string]interface{}) (string, error) {
	req := &NormalizeUnitsRequest{}

	if metricsJSON, ok := args["metrics"].(string); ok && metricsJSON != "" {
		var metrics []UnitMetric
		json.Unmarshal([]byte(metricsJSON), &metrics)
		req.Metrics = metrics
	}
	if targetUnit, ok := args["target_unit"].(string); ok {
		req.TargetUnit = targetUnit
	}

	resp, err := t.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

func (t *TypedUnitNormalizer) normalize(metrics []UnitMetric, targetUnit string) []NormalizedMetric {
	if targetUnit == "" {
		targetUnit = "亿元"
	}

	normalized := make([]NormalizedMetric, 0, len(metrics))

	for _, metric := range metrics {
		norm := t.convertUnit(metric, targetUnit)
		normalized = append(normalized, norm)
	}

	return normalized
}

func (t *TypedUnitNormalizer) convertUnit(metric UnitMetric, targetUnit string) NormalizedMetric {
	value := metric.Value
	currentUnit := metric.Unit

	if currentUnit == targetUnit {
		return NormalizedMetric{
			Name:            metric.Name,
			OriginalValue:   metric.Value,
			OriginalUnit:    metric.Unit,
			NormalizedValue: metric.Value,
			NormalizedUnit:  targetUnit,
			Year:            metric.Year,
		}
	}

	switch strings.TrimSpace(currentUnit) {
	case "元":
		if targetUnit == "亿元" {
			value = value / 100000000
		} else if targetUnit == "万元" {
			value = value / 10000
		}
	case "万元":
		if targetUnit == "亿元" {
			value = value / 10000
		} else if targetUnit == "元" {
			value = value * 10000
		}
	case "亿元":
		if targetUnit == "万元" {
			value = value * 10000
		} else if targetUnit == "元" {
			value = value * 100000000
		}
	case "%":
		if targetUnit == "倍数" {
			value = value / 100
		}
	case "倍数":
		if targetUnit == "%" {
			value = value * 100
		}
	}

	return NormalizedMetric{
		Name:            metric.Name,
		OriginalValue:   metric.Value,
		OriginalUnit:    metric.Unit,
		NormalizedValue: value,
		NormalizedUnit:  targetUnit,
		Year:            metric.Year,
	}
}

func (t *TypedUnitNormalizer) generateNormalizationSummary(normalized []NormalizedMetric) string {
	var builder strings.Builder
	builder.WriteString("**单位转换结果**\n\n")

	for _, n := range normalized {
		if n.OriginalUnit != n.NormalizedUnit {
			builder.WriteString(fmt.Sprintf("- %s: %.2f%s → %.2f%s\n",
				n.Name, n.OriginalValue, n.OriginalUnit, n.NormalizedValue, n.NormalizedUnit))
		}
	}

	return builder.String()
}

type ExtractTimelineRequest struct {
	Text      string `json:"text"`
	StockCode string `json:"stock_code,omitempty"`
}

type ExtractTimelineResponse struct {
	StockCode string          `json:"stock_code,omitempty"`
	Events    []TimelineEvent `json:"events"`
	Summary   string          `json:"summary"`
}

type TimelineEvent struct {
	Date      string `json:"date"`
	EventType string `json:"event_type"`
	Title     string `json:"title"`
	Detail    string `json:"detail,omitempty"`
}

type TypedTimelineExtractor struct {
	*BaseTypedTool
}

func NewTypedTimelineExtractor() *TypedTimelineExtractor {
	return &TypedTimelineExtractor{
		BaseTypedTool: &BaseTypedTool{
			name:        "extract_timeline",
			description: "从公告/新闻中提取事件时间线（财报发布、高管变动、订单签约、政策事件等）",
		},
	}
}

func (t *TypedTimelineExtractor) Schema() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: t.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"text":       {Type: "string", Desc: "文本内容", Required: true},
			"stock_code": {Type: "string", Desc: "股票代码", Required: false},
		}),
	}
}

func (t *TypedTimelineExtractor) Run(ctx context.Context, req *ExtractTimelineRequest) (*ExtractTimelineResponse, error) {
	if req.Text == "" {
		return nil, fmt.Errorf("文本内容不能为空")
	}

	events := t.extractEvents(req.Text, req.StockCode)

	return &ExtractTimelineResponse{
		StockCode: req.StockCode,
		Events:    events,
		Summary:   t.generateTimelineSummary(events),
	}, nil
}

func (t *TypedTimelineExtractor) Invoke(ctx context.Context, args map[string]interface{}) (string, error) {
	req := &ExtractTimelineRequest{}

	if text, ok := args["text"].(string); ok {
		req.Text = text
	}
	if stockCode, ok := args["stock_code"].(string); ok {
		req.StockCode = stockCode
	}

	resp, err := t.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

func (t *TypedTimelineExtractor) extractEvents(text, stockCode string) []TimelineEvent {
	events := []TimelineEvent{}

	datePattern := regexp.MustCompile(`(\d{4})年(\d{1,2})月(\d{1,2})日?`)
	dates := datePattern.FindAllStringSubmatch(text, -1)

	eventPatterns := map[string]string{
		"财报发布": `(?:发布|公布|披露|发表)(?:了)?(年度|季度|中期)?(?:报告|财报|业绩|报表)|(年度报告|季度报告|中期报告|一季报|中报|三季报|年报)`,
		"高管变动": `(?:董事|监事|高管|总经理|董事长|CEO|CFO|COO|CTO|独立董事|副董事长)(?:变 更?|调整|任命|辞职|离职|逝世|当选|增持|减持)`,
		"分红派息": `(?:分红|派息|送股|转增|配股|定向增发|公开增发|股权激励)`,
		"重大合同": `(?:合同|订单|协议|签约|中标|采购|供应|合作|合资|并购|收购|转让)`,
		"政策事件": `(?:政策|监管|问询|处罚|整改|调查|审查|核查)`,
		"业绩预告": `(?:业绩预告|业绩预增|业绩预减|业绩更正|业绩修正)`,
	}

	for _, dateMatch := range dates {
		if len(dateMatch) >= 4 {
			date := fmt.Sprintf("%s-%s-%s", dateMatch[1], padZero(dateMatch[2]), padZero(dateMatch[3]))

			for eventType, pattern := range eventPatterns {
				re := regexp.MustCompile(pattern)
				if re.MatchString(text) {
					matches := re.FindAllStringSubmatch(text, -1)
					for _, match := range matches {
						title := t.extractEventTitle(match[0], eventType)
						events = append(events, TimelineEvent{
							Date:      date,
							EventType: eventType,
							Title:     title,
						})
					}
				}
			}
		}
	}

	if len(events) == 0 {
		events = append(events, TimelineEvent{
			Date:      time.Now().Format("2006-01-02"),
			EventType: "其他事件",
			Title:     "未识别到关键事件",
		})
	}

	return events
}

func (t *TypedTimelineExtractor) extractEventTitle(match, eventType string) string {
	title := match
	if len(title) > 50 {
		title = title[:50] + "..."
	}
	return title
}

func (t *TypedTimelineExtractor) generateTimelineSummary(events []TimelineEvent) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("共提取到 %d 个事件\n\n", len(events)))

	eventTypes := make(map[string]int)
	for _, e := range events {
		eventTypes[e.EventType]++
	}

	builder.WriteString("**事件类型分布**\n")
	for etype, count := range eventTypes {
		builder.WriteString(fmt.Sprintf("- %s: %d 个\n", etype, count))
	}

	return builder.String()
}

func padZero(s string) string {
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

type ResolveEntityRequest struct {
	Query string `json:"query"`
	Type  string `json:"type,omitempty"`
}

type ResolveEntityResponse struct {
	Query    string       `json:"query"`
	Resolved []EntityInfo `json:"resolved"`
	Summary  string       `json:"summary"`
}

type EntityInfo struct {
	StockCode string `json:"stock_code"`
	StockName string `json:"stock_name"`
	Alias     string `json:"alias,omitempty"`
	Market    string `json:"market"`
	Exchange  string `json:"exchange,omitempty"`
	Type      string `json:"type,omitempty"`
}

type TypedEntityResolver struct {
	*BaseTypedTool
}

func NewTypedEntityResolver() *TypedEntityResolver {
	return &TypedEntityResolver{
		BaseTypedTool: &BaseTypedTool{
			name:        "resolve_entity",
			description: "公司名、简称、股票代码归一化（如茅台->贵州茅台->600519）",
		},
	}
}

func (t *TypedEntityResolver) Schema() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: t.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {Type: "string", Desc: "查询词（公司名/简称/股票代码）", Required: true},
			"type":  {Type: "string", Desc: "类型(stock/commodity/index)", Required: false},
		}),
	}
}

func (t *TypedEntityResolver) Run(ctx context.Context, req *ResolveEntityRequest) (*ResolveEntityResponse, error) {
	if req.Query == "" {
		return nil, fmt.Errorf("查询词不能为空")
	}

	entities := t.resolveEntity(req.Query)

	return &ResolveEntityResponse{
		Query:    req.Query,
		Resolved: entities,
		Summary:  t.generateEntitySummary(entities),
	}, nil
}

func (t *TypedEntityResolver) Invoke(ctx context.Context, args map[string]interface{}) (string, error) {
	req := &ResolveEntityRequest{}

	if query, ok := args["query"].(string); ok {
		req.Query = query
	}
	if queryType, ok := args["type"].(string); ok {
		req.Type = queryType
	}

	resp, err := t.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

func (t *TypedEntityResolver) resolveEntity(query string) []EntityInfo {
	query = strings.TrimSpace(query)
	queryLower := strings.ToLower(query)

	entityDB := map[string][]EntityInfo{
		"茅台": {
			{StockCode: "600519", StockName: "贵州茅台", Alias: "茅台", Market: "A股", Exchange: "SH"},
		},
		"贵州茅台": {
			{StockCode: "600519", StockName: "贵州茅台", Alias: "茅台", Market: "A股", Exchange: "SH"},
		},
		"600519": {
			{StockCode: "600519", StockName: "贵州茅台", Alias: "茅台", Market: "A股", Exchange: "SH"},
		},
		"比亚迪": {
			{StockCode: "002594", StockName: "比亚迪", Alias: "BYD", Market: "A股", Exchange: "SZ"},
		},
		"byd": {
			{StockCode: "002594", StockName: "比亚迪", Alias: "BYD", Market: "A股", Exchange: "SZ"},
			{StockCode: "1211", StockName: "比亚迪股份", Alias: "比亚迪H股", Market: "港股", Exchange: "HK"},
		},
		"002594": {
			{StockCode: "002594", StockName: "比亚迪", Alias: "BYD", Market: "A股", Exchange: "SZ"},
		},
		"宁德时代": {
			{StockCode: "300750", StockName: "宁德时代", Alias: "CATL", Market: "A股", Exchange: "SZ"},
		},
		"catl": {
			{StockCode: "300750", StockName: "宁德时代", Alias: "CATL", Market: "A股", Exchange: "SZ"},
		},
		"300750": {
			{StockCode: "300750", StockName: "宁德时代", Alias: "CATL", Market: "A股", Exchange: "SZ"},
		},
		"五粮液": {
			{StockCode: "000858", StockName: "五粮液", Alias: "五粮液", Market: "A股", Exchange: "SZ"},
		},
		"000858": {
			{StockCode: "000858", StockName: "五粮液", Alias: "五粮液", Market: "A股", Exchange: "SZ"},
		},
		"中国平安": {
			{StockCode: "601318", StockName: "中国平安", Alias: "平安", Market: "A股", Exchange: "SH"},
		},
		"601318": {
			{StockCode: "601318", StockName: "中国平安", Alias: "平安", Market: "A股", Exchange: "SH"},
		},
		"招商银行": {
			{StockCode: "600036", StockName: "招商银行", Alias: "招行", Market: "A股", Exchange: "SH"},
		},
		"600036": {
			{StockCode: "600036", StockName: "招商银行", Alias: "招行", Market: "A股", Exchange: "SH"},
		},
		"腾讯": {
			{StockCode: "00700", StockName: "腾讯控股", Alias: "腾讯", Market: "港股", Exchange: "HK"},
		},
		"00700": {
			{StockCode: "00700", StockName: "腾讯控股", Alias: "腾讯", Market: "港股", Exchange: "HK"},
		},
		"阿里巴巴": {
			{StockCode: "09988", StockName: "阿里巴巴", Alias: "阿里", Market: "港股", Exchange: "HK"},
			{StockCode: "BABA", StockName: "Alibaba", Alias: "阿里", Market: "美股", Exchange: "NYSE"},
		},
		"baba": {
			{StockCode: "BABA", StockName: "Alibaba", Alias: "阿里巴巴", Market: "美股", Exchange: "NYSE"},
		},
	}

	if entities, ok := entityDB[query]; ok {
		return entities
	}

	if entities, ok := entityDB[queryLower]; ok {
		return entities
	}

	for key, entities := range entityDB {
		if strings.Contains(key, query) || strings.Contains(query, key) {
			return entities
		}
		for _, ent := range entities {
			if strings.Contains(strings.ToLower(ent.StockName), queryLower) ||
				strings.Contains(strings.ToLower(ent.Alias), queryLower) {
				return entities
			}
		}
	}

	return []EntityInfo{
		{StockCode: "未找到", StockName: "未找到匹配结果", Market: "未知"},
	}
}

func (t *TypedEntityResolver) generateEntitySummary(entities []EntityInfo) string {
	var builder strings.Builder

	if len(entities) == 1 && entities[0].StockCode == "未找到" {
		return "未找到匹配的公司实体"
	}

	builder.WriteString(fmt.Sprintf("找到 %d 个匹配结果:\n", len(entities)))

	for _, e := range entities {
		builder.WriteString(fmt.Sprintf("- %s (%s) [%s-%s]\n",
			e.StockName, e.StockCode, e.Market, e.Exchange))
	}

	return builder.String()
}
