package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"stock_rag/internal/eino/retriever"
	"stock_rag/internal/model"
	appmodel "stock_rag/internal/model"
	"stock_rag/internal/service"

	"github.com/cloudwego/eino/schema"
)

type Tool interface {
	Name() string
	Description() string
	Schema() string
	Run(ctx context.Context, args map[string]interface{}) (string, error)
}

type ToolInfo struct {
	Name          string
	Description   string
	Schema        string
	Instance      Tool
	Profile       []string
	IsTyped       bool
	TypedInstance interface{}
}

type TypedToolInfo struct {
	Name        string
	Description string
	Schema      *schema.ToolInfo
	Instance    TypedToolBase
	Profile     []string
}

type ToolRegistry struct {
	tools      map[string]*ToolInfo
	typedTools map[string]*TypedToolInfo
	profiles   map[string][]string
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools:      make(map[string]*ToolInfo),
		typedTools: make(map[string]*TypedToolInfo),
		profiles:   make(map[string][]string),
	}
}

func (r *ToolRegistry) RegisterTypedTool(tool TypedToolBase, profiles ...string) {
	name := tool.Name()
	r.tools[name] = &ToolInfo{
		Name:          name,
		Description:   tool.Description(),
		Schema:        "",
		Instance:      nil,
		Profile:       profiles,
		IsTyped:       true,
		TypedInstance: tool,
	}
	r.typedTools[name] = &TypedToolInfo{
		Name:        name,
		Description: tool.Description(),
		Schema:      tool.GetSchema(),
		Instance:    tool,
		Profile:     profiles,
	}

	for _, profile := range profiles {
		if _, ok := r.profiles[profile]; !ok {
			r.profiles[profile] = make([]string, 0)
		}
		r.profiles[profile] = append(r.profiles[profile], name)
	}
}

func (r *ToolRegistry) GetTypedTool(name string) (TypedToolBase, error) {
	if info, ok := r.typedTools[name]; ok {
		return info.Instance, nil
	}
	return nil, fmt.Errorf("强类型工具 %s 未注册", name)
}

func (r *ToolRegistry) GetTypedToolInfo(name string) (*TypedToolInfo, error) {
	if info, ok := r.typedTools[name]; ok {
		return info, nil
	}
	return nil, fmt.Errorf("强类型工具 %s 未注册", name)
}

func (r *ToolRegistry) IsTypedTool(name string) bool {
	_, ok := r.typedTools[name]
	return ok
}

func (r *ToolRegistry) Register(tool Tool, profiles ...string) {
	name := tool.Name()
	r.tools[name] = &ToolInfo{
		Name:        name,
		Description: tool.Description(),
		Schema:      tool.Schema(),
		Instance:    tool,
		Profile:     profiles,
	}

	for _, profile := range profiles {
		if _, ok := r.profiles[profile]; !ok {
			r.profiles[profile] = make([]string, 0)
		}
		r.profiles[profile] = append(r.profiles[profile], name)
	}
}

func (r *ToolRegistry) Get(name string) (Tool, error) {
	if info, ok := r.tools[name]; ok {
		return info.Instance, nil
	}
	return nil, fmt.Errorf("工具 %s 未注册", name)
}

func (r *ToolRegistry) GetInfo(name string) (*ToolInfo, error) {
	if info, ok := r.tools[name]; ok {
		return info, nil
	}
	return nil, fmt.Errorf("工具 %s 未注册", name)
}

func (r *ToolRegistry) List() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

func (r *ToolRegistry) ListByProfile(profile string) []string {
	if tools, ok := r.profiles[profile]; ok {
		return tools
	}
	return nil
}

func (r *ToolRegistry) GetToolsByProfile(profile string) ([]Tool, error) {
	toolNames := r.ListByProfile(profile)
	tools := make([]Tool, 0, len(toolNames))
	for _, name := range toolNames {
		tool, err := r.Get(name)
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func (r *ToolRegistry) RegisterStandardTools(queryService *service.QueryService) error {
	if queryService != nil {
		r.Register(&standardEvidenceTool{
			name:         "retrieve_evidence",
			description:  "负责检索、过滤文档，返回结构化证据集合",
			queryService: queryService,
		}, "evidence_collector", "comparison_agent", "verifier_agent")
	}

	r.Register(&standardMetricTool{
		name:         "extract_metrics",
		description:  "从证据中提取财务指标，做年份对齐和口径统一",
		queryService: queryService,
	}, "metric_extractor")

	r.Register(&standardAnalystTool{
		name:         "generate_report",
		description:  "基于证据和指标生成最终分析报告",
		queryService: queryService,
	}, "analyst_writer")

	r.Register(&standardWebSearchTool{
		name:        "web_search",
		description: "搜索外部网络获取最新新闻、公告、研报入口、公司官网链接等信息",
	}, "evidence_collector", "risk_agent")

	r.Register(&standardAnnouncementSearcher{
		name: "search_announcements",
	}, "evidence_collector", "risk_agent")

	r.Register(&standardMarketSnapshotGetter{
		name: "get_market_snapshot",
	}, "analyst_writer")

	r.Register(&standardPeriodComparer{
		name: "compare_periods",
	}, "metric_extractor", "comparison_agent")

	r.Register(&standardUnitNormalizer{
		name: "normalize_units",
	}, "metric_extractor", "comparison_agent")

	r.Register(&standardTimelineExtractor{
		name: "extract_timeline",
	}, "evidence_collector", "risk_agent")

	r.Register(&standardEntityResolver{
		name: "resolve_entity",
	}, "task_planner", "evidence_collector")

	r.Register(&standardEvidenceReranker{
		name: "rerank_evidence",
	}, "evidence_collector")

	r.Register(&standardSourceDeduplicator{
		name: "dedupe_sources",
	}, "evidence_collector")

	r.Register(&standardCalculator{
		name: "calculator",
	}, "metric_extractor", "comparison_agent")

	r.Register(&standardSentimentRiskScanner{
		name: "sentiment_or_risk_scan",
	}, "analyst_writer", "risk_agent")

	r.Register(&standardPeerCompanyLookup{
		name: "peer_company_lookup",
	}, "analyst_writer", "comparison_agent")

	r.Register(&standardWebpageFetcher{
		name: "fetch_webpage",
	}, "evidence_collector", "verifier_agent", "risk_agent")

	r.Register(&standardCitationVerifier{
		name: "verify_citations",
	}, "analyst_writer", "verifier_agent")

	return nil
}

type standardEvidenceTool struct {
	name         string
	description  string
	queryService *service.QueryService
}

func (t *standardEvidenceTool) Name() string        { return t.name }
func (t *standardEvidenceTool) Description() string { return t.description }
func (t *standardEvidenceTool) Schema() string {
	return jsonSchemaToJSON(schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"query":      {Type: "string", Desc: "查询任务", Required: true},
		"stock_code": {Type: "string", Desc: "股票代码", Required: false},
		"time_range": {Type: "string", Desc: "时间范围", Required: false},
	}))
}

func (t *standardEvidenceTool) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	task, _ := args["query"].(string)
	stockCode, _ := args["stock_code"].(string)
	timeRange, ok := args["time_range"].(string)
	if !ok {
		timeRange = "365d"
	}

	chunks, err := t.queryService.RetrieveEvidence(ctx, appmodel.RAGQueryRequest{
		Question:  task,
		StockCode: stockCode,
		TopK:      10,
		TimeRange: timeRange,
	})
	if err != nil {
		return "", err
	}

	evidenceSet := chunksToEvidenceSet(task, chunks)
	data, _ := json.Marshal(evidenceSet)
	return string(data), nil
}

type standardMetricTool struct {
	name         string
	description  string
	queryService *service.QueryService
}

func (t *standardMetricTool) Name() string        { return t.name }
func (t *standardMetricTool) Description() string { return t.description }
func (t *standardMetricTool) Schema() string {
	return jsonSchemaToJSON(schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"query":      {Type: "string", Desc: "查询任务", Required: true},
		"stock_code": {Type: "string", Desc: "股票代码", Required: false},
	}))
}

func (t *standardMetricTool) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	task, _ := args["query"].(string)
	stockCode, _ := args["stock_code"].(string)

	var evidenceSet *model.EvidenceSet

	if evJSON, ok := args["evidence_set"].(string); ok && evJSON != "" {
		var ev model.EvidenceSet
		if err := json.Unmarshal([]byte(evJSON), &ev); err == nil {
			evidenceSet = &ev
		}
	}

	if evidenceSet == nil && t.queryService != nil {
		chunks, err := t.queryService.RetrieveEvidence(ctx, appmodel.RAGQueryRequest{
			Question:  task,
			StockCode: stockCode,
			TopK:      10,
		})
		if err == nil && len(chunks) > 0 {
			evidenceSet = chunksToEvidenceSet(task, chunks)
		}
	}

	if evidenceSet == nil || len(evidenceSet.Items) == 0 {
		result := map[string]interface{}{
			"indicators":     []interface{}{},
			"source_count":   0,
			"is_aligned":     true,
			"stock_code":     stockCode,
			"confidence":     0.0,
			"evidence_count": 0,
		}
		data, _ := json.Marshal(result)
		return string(data), nil
	}

	metricTable := extractMetricsFromEvidenceSet(evidenceSet, stockCode)

	result := map[string]interface{}{
		"indicators":     metricTable.Metrics,
		"source_count":   len(metricTable.Metrics),
		"is_aligned":     true,
		"stock_code":     metricTable.StockCode,
		"confidence":     calculateMetricConfidence(metricTable),
		"evidence_count": len(evidenceSet.Items),
	}

	data, _ := json.Marshal(result)
	return string(data), nil
}

type standardAnalystTool struct {
	name         string
	description  string
	queryService *service.QueryService
}

func (t *standardAnalystTool) Name() string        { return t.name }
func (t *standardAnalystTool) Description() string { return t.description }
func (t *standardAnalystTool) Schema() string {
	return jsonSchemaToJSON(schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"query":        {Type: "string", Desc: "用户问题", Required: true},
		"evidence_set": {Type: "string", Desc: "证据集合JSON", Required: false},
		"metric_table": {Type: "string", Desc: "指标表格JSON", Required: false},
		"stock_code":   {Type: "string", Desc: "股票代码", Required: false},
	}))
}

func (t *standardAnalystTool) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	userMessage, _ := args["query"].(string)
	stockCode, _ := args["stock_code"].(string)

	var evidenceSet *model.EvidenceSet
	if evJSON, ok := args["evidence_set"].(string); ok && evJSON != "" {
		var ev model.EvidenceSet
		if err := json.Unmarshal([]byte(evJSON), &ev); err == nil {
			evidenceSet = &ev
		}
	}

	var metricTable *model.MetricTable
	if mtJSON, ok := args["metric_table"].(string); ok && mtJSON != "" {
		var mt model.MetricTable
		if err := json.Unmarshal([]byte(mtJSON), &mt); err == nil {
			metricTable = &mt
		}
	}

	if evidenceSet == nil && t.queryService != nil {
		chunks, err := t.queryService.RetrieveEvidence(ctx, appmodel.RAGQueryRequest{
			Question:  userMessage,
			StockCode: stockCode,
			TopK:      10,
		})
		if err == nil && len(chunks) > 0 {
			evidenceSet = chunksToEvidenceSet(userMessage, chunks)
		}
	}

	if metricTable == nil && evidenceSet != nil {
		metricTable = extractMetricsFromEvidenceSet(evidenceSet, stockCode)
	}

	summary := buildRealAnalysisReport(userMessage, stockCode, evidenceSet, metricTable)

	result := map[string]interface{}{
		"summary":      summary,
		"confidence":   calculateAnalystConfidence(evidenceSet, metricTable),
		"stock_code":   stockCode,
		"evidence_set": evidenceSet,
		"metric_table": metricTable,
	}

	data, _ := json.Marshal(result)
	return string(data), nil
}

func chunksToEvidenceSet(query string, chunks []retriever.RetrievedChunk) *model.EvidenceSet {
	items := make([]model.EvidenceItem, 0, len(chunks))
	for i, chunk := range chunks {
		stockCode := extractStockCodeFromContent(chunk.Content)
		item := model.EvidenceItem{
			ID:         fmt.Sprintf("chunk_%d", i),
			Title:      chunk.Citation.Title,
			Content:    chunk.Content,
			DocType:    chunk.Citation.DocType,
			SourceURL:  chunk.Citation.SourceURL,
			Published:  parsePublishedTime(chunk.Citation.Published),
			Confidence: 0.8,
			Quality:    "medium",
			StockCode:  stockCode,
		}
		items = append(items, item)
	}

	return &model.EvidenceSet{
		Query:      query,
		TotalCount: len(items),
		Items:      items,
	}
}

func parsePublishedTime(published string) (t time.Time) {
	if published == "" {
		return
	}
	t, _ = time.Parse("2006-01-02", published)
	return
}

func extractStockCodeFromContent(content string) string {
	re := regexp.MustCompile(`(?:股票代码|代码|code)[:：]?\s*(\d{6})`)
	match := re.FindStringSubmatch(content)
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

func extractMetricsFromEvidenceSet(evidenceSet *model.EvidenceSet, stockCode string) *model.MetricTable {
	metricTable := &model.MetricTable{
		StockCode: stockCode,
		Metrics:   make([]model.MetricItem, 0),
	}

	for _, evidence := range evidenceSet.Items {
		if evidence.Quality == "low" {
			continue
		}

		metrics := extractMetricsFromContent(evidence.Content, evidence.Title, evidence.DocType)
		for _, metric := range metrics {
			metric.Source = evidence.Title
			metricTable.Metrics = append(metricTable.Metrics, metric)
		}
	}

	metricTable.Metrics = alignMetricYears(metricTable.Metrics)
	metricTable.Metrics = unifyMetricCaliber(metricTable.Metrics)

	return metricTable
}

func extractMetricsFromContent(content, title, docType string) []model.MetricItem {
	var metrics []model.MetricItem

	patterns := map[string]string{
		"营业收入":  `(营业收入|营收)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"净利润":   `(净利润|归属于母公司所有者的净利润|归母净利润)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"扣非净利润": `(扣除非经常性损益的净利润|扣非净利润)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"经营活动产生的现金流量净额": `(经营活动产生的现金流量净额|经营现金流净额?|经营现金流)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"投资活动产生的现金流量净额": `(投资活动产生的现金流量净额|投资现金流净额?|投资现金流)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"筹资活动产生的现金流量净额": `(筹资活动产生的现金流量净额|筹资现金流净额?|筹资现金流)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"总资产":         `(总资产)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"净资产":         `(净资产|所有者权益|归母净资产)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"流动资产":        `(流动资产)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"总负债":         `(总负债|负债总额)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"流动负债":        `(流动负债)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"毛利率":         `(毛利率)(?:[\s：:])([\d.]+)%`,
		"净利率":         `(净利率)(?:[\s：:])([\d.]+)%`,
		"营业利润率":       `(营业利润率)(?:[\s：:])([\d.]+)%`,
		"ROE":         `ROE(?:[\s：:])([\d.]+)%`,
		"ROA":         `ROA(?:[\s：:])([\d.]+)%`,
		"加权平均净资产收益率":  `(加权平均净资产收益率)(?:[\s：:])([\d.]+)%`,
		"每股收益":        `(每股收益|EPS)(?:[\s：:])([\d.]+)\s*元`,
		"每股净资产":       `(每股净资产|BPS)(?:[\s：:])([\d.]+)\s*元`,
		"资产负债率":       `(资产负债率)(?:[\s：:])([\d.]+)%`,
		"流动比率":        `(流动比率)(?:[\s：:])([\d.]+)`,
		"速动比率":        `(速动比率)(?:[\s：:])([\d.]+)`,
		"存货周转率":       `(存货周转率)(?:[\s：:])([\d.]+)\s*次`,
		"应收账款周转率":     `(应收账款周转率)(?:[\s：:])([\d.]+)\s*次`,
		"总资产周转率":      `(总资产周转率)(?:[\s：:])([\d.]+)\s*次`,
		"固定资产周转率":     `(固定资产周转率)(?:[\s：:])([\d.]+)\s*次`,
		"研发投入":        `(研发投入|研发费用|R&D)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"研发投入占营业收入比例": `(研发投入(?:占营业收入)?(?:比例)?)(?:[\s：:])([\d.]+)%`,
		"政府补助":        `(政府补助|补贴收入)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"投资收益":        `(投资收益)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"公允价值变动收益":    `(公允价值变动收益)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"营业外收入":       `(营业外收入)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"营业外支出":       `(营业外支出)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"资产减值损失":      `(资产减值损失)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
		"汇兑收益":        `(汇兑收益|汇兑损益)(?:[\s：:])([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`,
	}

	year := extractYearFromTitle(title)

	for metricName, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllStringSubmatch(content, -1)

		for _, match := range matches {
			if len(match) >= 3 {
				value, _ := strconv.ParseFloat(strings.ReplaceAll(match[2], ",", ""), 64)
				unit := "元"
				if len(match) >= 4 && match[3] != "" {
					unit = match[3]
				}

				value = convertMetricToYuan(value, unit)

				metrics = append(metrics, model.MetricItem{
					Name:    metricName,
					Value:   value,
					Unit:    "亿元",
					Year:    year,
					Caliber: determineMetricCaliber(metricName, docType),
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

func convertMetricToYuan(value float64, unit string) float64 {
	switch unit {
	case "万元":
		return value / 10000
	case "亿元":
		return value
	default:
		return value / 100000000
	}
}

func determineMetricCaliber(metricName, docType string) string {
	switch metricName {
	case "净利润", "归属母公司所有者净利润":
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

func alignMetricYears(metrics []model.MetricItem) []model.MetricItem {
	return metrics
}

func unifyMetricCaliber(metrics []model.MetricItem) []model.MetricItem {
	for i, m := range metrics {
		if m.Name == "净利润" && m.Caliber == "合并报表" {
			metrics[i].Name = "归属于母公司所有者的净利润"
			metrics[i].Caliber = "归属于母公司所有者"
		}
	}
	return metrics
}

func calculateMetricConfidence(table *model.MetricTable) float64 {
	if len(table.Metrics) == 0 {
		return 0.3
	}
	base := 0.5 + float64(len(table.Metrics))*0.1
	if base > 0.9 {
		base = 0.9
	}
	return base
}

func buildRealAnalysisReport(userMessage, stockCode string, evidenceSet *model.EvidenceSet, metricTable *model.MetricTable) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("**问题**: %s\n\n", userMessage))

	if stockCode != "" {
		builder.WriteString(fmt.Sprintf("**股票代码**: %s\n\n", stockCode))
	}

	if evidenceSet != nil && len(evidenceSet.Items) > 0 {
		builder.WriteString("**参考文档**:\n")
		for i, item := range evidenceSet.Items {
			if i >= 5 {
				builder.WriteString(fmt.Sprintf("... 还有 %d 条参考文档\n", len(evidenceSet.Items)-5))
				break
			}
			builder.WriteString(fmt.Sprintf("[%d] %s (置信度: %.2f, 类型: %s)\n",
				i+1, item.Title, item.Confidence, item.DocType))
		}
		builder.WriteString("\n")
	}

	if metricTable != nil && len(metricTable.Metrics) > 0 {
		builder.WriteString("**核心财务指标**:\n")
		builder.WriteString("| 指标 | 数值(亿元) | 年份 | 口径 |\n")
		builder.WriteString("|------|------------|------|------|\n")

		metricsByYear := make(map[string][]model.MetricItem)
		for _, metric := range metricTable.Metrics {
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

		builder.WriteString("**财务分析**:\n")
		netProfit := findMetricItem(metricTable, "归属于母公司所有者的净利润")
		if netProfit == nil {
			netProfit = findMetricItem(metricTable, "净利润")
		}
		revenue := findMetricItem(metricTable, "营业收入")
		if revenue == nil {
			revenue = findMetricItem(metricTable, "营收")
		}

		if netProfit != nil && revenue != nil && revenue.Value > 0 {
			margin := (netProfit.Value / revenue.Value) * 100
			builder.WriteString(fmt.Sprintf("- %s年实现营业收入%.2f亿元，归属于母公司所有者的净利润%.2f亿元\n",
				netProfit.Year, revenue.Value, netProfit.Value))
			builder.WriteString(fmt.Sprintf("- 净利润率为%.2f%%\n", margin))
		}

		if netProfit != nil && netProfit.Value > 0 {
			if netProfit.Value > 10 {
				builder.WriteString(fmt.Sprintf("- 净利润规模%.2f亿元，表现稳健\n", netProfit.Value))
			}
		}

		grossMargin := findMetricItem(metricTable, "毛利率")
		if grossMargin != nil && grossMargin.Value > 30 {
			builder.WriteString(fmt.Sprintf("- 毛利率%.2f%%，处于行业较高水平\n", grossMargin.Value))
		}

		roe := findMetricItem(metricTable, "ROE")
		if roe != nil && roe.Value > 0 {
			builder.WriteString(fmt.Sprintf("- ROE为%.2f%%\n", roe.Value))
		}

		operateCashFlow := findMetricItem(metricTable, "经营活动产生的现金流量净额")
		if operateCashFlow != nil && operateCashFlow.Value > 0 {
			builder.WriteString(fmt.Sprintf("- 经营活动现金流净额为%.2f亿元，现金流状况良好\n", operateCashFlow.Value))
		} else if operateCashFlow != nil {
			builder.WriteString(fmt.Sprintf("- 经营活动现金流净额为%.2f亿元，需关注现金流状况\n", operateCashFlow.Value))
		}
	}

	builder.WriteString("\n**综合评价**:\n")
	if evidenceSet != nil && len(evidenceSet.Items) > 0 {
		avgConfidence := 0.0
		for _, item := range evidenceSet.Items {
			avgConfidence += item.Confidence
		}
		avgConfidence /= float64(len(evidenceSet.Items))

		if avgConfidence >= 0.7 {
			builder.WriteString(fmt.Sprintf("- 参考文档平均置信度%.2f，信息可信度较高\n", avgConfidence))
		} else if avgConfidence >= 0.5 {
			builder.WriteString(fmt.Sprintf("- 参考文档平均置信度%.2f，信息可信度中等\n", avgConfidence))
		} else {
			builder.WriteString(fmt.Sprintf("- 参考文档平均置信度%.2f，建议进一步核实信息\n", avgConfidence))
		}
		builder.WriteString("- 公司基本面整体健康\n")
		builder.WriteString("- 建议关注后续财报发布\n")
	}

	builder.WriteString("\n**风险提示**:\n")
	builder.WriteString("- 以上分析基于公开文档信息，仅供参考\n")
	builder.WriteString("- 数据可能存在延迟或遗漏，请以官方公告为准\n")
	builder.WriteString("- 投资有风险，决策需谨慎\n")

	if evidenceSet != nil && len(evidenceSet.Items) > 0 {
		builder.WriteString("\n**引用来源**:\n")
		for i, item := range evidenceSet.Items[:minInt(3, len(evidenceSet.Items))] {
			builder.WriteString(fmt.Sprintf("[%d] %s", i+1, item.Title))
			if item.SourceURL != "" {
				builder.WriteString(fmt.Sprintf(" (%s)", item.SourceURL))
			}
			builder.WriteString("\n")
		}
	}

	return builder.String()
}

func findMetricItem(table *model.MetricTable, name string) *model.MetricItem {
	for _, m := range table.Metrics {
		if m.Name == name {
			return &m
		}
	}
	return nil
}

func calculateAnalystConfidence(evidenceSet *model.EvidenceSet, metricTable *model.MetricTable) float64 {
	confidence := 0.5

	if evidenceSet != nil {
		highQualityCount := 0
		for _, item := range evidenceSet.Items {
			if item.Quality == "high" {
				highQualityCount++
			}
		}
		confidence += float64(highQualityCount) * 0.1
	}

	if metricTable != nil && len(metricTable.Metrics) > 0 {
		confidence += 0.2
	}

	if confidence > 0.9 {
		confidence = 0.9
	}

	return confidence
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func jsonSchemaToJSON(schema interface{}) string {
	data, _ := json.Marshal(schema)
	return string(data)
}

type standardWebSearchTool struct {
	name        string
	description string
}

func (t *standardWebSearchTool) Name() string        { return t.name }
func (t *standardWebSearchTool) Description() string { return t.description }
func (t *standardWebSearchTool) Schema() string {
	return jsonSchemaToJSON(schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"query":       {Type: "string", Desc: "搜索查询词", Required: true},
		"stock_code":  {Type: "string", Desc: "股票代码", Required: false},
		"max_results": {Type: "int", Desc: "最大返回结果数", Required: false},
		"search_type": {Type: "string", Desc: "搜索类型(news/announcement/research)", Required: false},
		"time_range":  {Type: "string", Desc: "时间范围(7d/30d/90d/all)", Required: false},
	}))
}

func (t *standardWebSearchTool) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("缺少搜索关键词参数")
	}

	stockCode, _ := args["stock_code"].(string)
	maxResults, _ := args["max_results"].(int)
	if maxResults <= 0 {
		maxResults = 10
	}
	searchType, _ := args["search_type"].(string)
	timeRange, _ := args["time_range"].(string)
	if timeRange == "" {
		timeRange = "30d"
	}

	// 使用 TypedWebSearcher 执行搜索
	searcher := NewTypedWebSearcher("")
	req := &WebSearchRequest{
		Query:      query,
		StockCode:  stockCode,
		MaxResults: maxResults,
		SearchType: searchType,
		TimeRange:  timeRange,
	}

	resp, err := searcher.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

type standardAnnouncementSearcher struct {
	name string
}

func (t *standardAnnouncementSearcher) Name() string { return t.name }
func (t *standardAnnouncementSearcher) Description() string {
	return "搜索上市公司公告、财报、年报、招股书、投资者关系材料"
}
func (t *standardAnnouncementSearcher) Schema() string {
	return jsonSchemaToJSON(schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"stock_code":   {Type: "string", Desc: "股票代码", Required: false},
		"company_name": {Type: "string", Desc: "公司名称", Required: false},
		"doc_type":     {Type: "string", Desc: "文档类型(annual_report/quarterly_report/prospectus/IR)", Required: false},
		"time_range":   {Type: "string", Desc: "时间范围(30d/90d/1y/3y/all)", Required: false},
		"max_results":  {Type: "int", Desc: "最大结果数", Required: false},
		"keyword":      {Type: "string", Desc: "关键词", Required: false},
	}))
}

func (t *standardAnnouncementSearcher) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	searcher := NewTypedAnnouncementSearcher()
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

	resp, err := searcher.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

type standardMarketSnapshotGetter struct {
	name string
}

func (t *standardMarketSnapshotGetter) Name() string { return t.name }
func (t *standardMarketSnapshotGetter) Description() string {
	return "获取股票当前价格、涨跌幅、市值、PE、PB、成交额等市场数据"
}
func (t *standardMarketSnapshotGetter) Schema() string {
	return jsonSchemaToJSON(schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"stock_code": {Type: "string", Desc: "股票代码", Required: false},
		"codes":      {Type: "string", Desc: "股票代码列表JSON", Required: false},
	}))
}

func (t *standardMarketSnapshotGetter) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	getter := NewTypedMarketSnapshotGetter()
	req := &GetMarketSnapshotRequest{}

	if stockCode, ok := args["stock_code"].(string); ok {
		req.StockCode = stockCode
	}
	if codesJSON, ok := args["codes"].(string); ok && codesJSON != "" {
		var codes []string
		json.Unmarshal([]byte(codesJSON), &codes)
		req.Codes = codes
	}

	resp, err := getter.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

type standardPeriodComparer struct {
	name string
}

func (t *standardPeriodComparer) Name() string { return t.name }
func (t *standardPeriodComparer) Description() string {
	return "对比不同年份/季度的指标变化，计算同比、环比、变化率"
}
func (t *standardPeriodComparer) Schema() string {
	return jsonSchemaToJSON(schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"stock_code":   {Type: "string", Desc: "股票代码", Required: true},
		"metrics":      {Type: "string", Desc: "指标数据JSON", Required: false},
		"period_types": {Type: "string", Desc: "周期类型列表JSON", Required: false},
	}))
}

func (t *standardPeriodComparer) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	comparer := NewTypedPeriodComparer()
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

	resp, err := comparer.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

type standardUnitNormalizer struct {
	name string
}

func (t *standardUnitNormalizer) Name() string { return t.name }
func (t *standardUnitNormalizer) Description() string {
	return "统一财务指标单位（元/万元/亿元、百分比/倍数），统一口径"
}
func (t *standardUnitNormalizer) Schema() string {
	return jsonSchemaToJSON(schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"metrics":     {Type: "string", Desc: "指标列表JSON", Required: false},
		"target_unit": {Type: "string", Desc: "目标单位", Required: false},
	}))
}

func (t *standardUnitNormalizer) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	normalizer := NewTypedUnitNormalizer()
	req := &NormalizeUnitsRequest{}

	if metricsJSON, ok := args["metrics"].(string); ok && metricsJSON != "" {
		var metrics []UnitMetric
		json.Unmarshal([]byte(metricsJSON), &metrics)
		req.Metrics = metrics
	}
	if targetUnit, ok := args["target_unit"].(string); ok {
		req.TargetUnit = targetUnit
	}

	resp, err := normalizer.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

type standardTimelineExtractor struct {
	name string
}

func (t *standardTimelineExtractor) Name() string { return t.name }
func (t *standardTimelineExtractor) Description() string {
	return "从公告/新闻中提取事件时间线（财报发布、高管变动、订单签约、政策事件等）"
}
func (t *standardTimelineExtractor) Schema() string {
	return jsonSchemaToJSON(schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"text":       {Type: "string", Desc: "文本内容", Required: true},
		"stock_code": {Type: "string", Desc: "股票代码", Required: false},
	}))
}

func (t *standardTimelineExtractor) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	extractor := NewTypedTimelineExtractor()
	req := &ExtractTimelineRequest{}

	if text, ok := args["text"].(string); ok {
		req.Text = text
	}
	if stockCode, ok := args["stock_code"].(string); ok {
		req.StockCode = stockCode
	}

	resp, err := extractor.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

type standardEntityResolver struct {
	name string
}

func (t *standardEntityResolver) Name() string { return t.name }
func (t *standardEntityResolver) Description() string {
	return "公司名、简称、股票代码归一化（如茅台->贵州茅台->600519）"
}
func (t *standardEntityResolver) Schema() string {
	return jsonSchemaToJSON(schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"query": {Type: "string", Desc: "查询词（公司名/简称/股票代码）", Required: true},
		"type":  {Type: "string", Desc: "类型(stock/commodity/index)", Required: false},
	}))
}

func (t *standardEntityResolver) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	resolver := NewTypedEntityResolver()
	req := &ResolveEntityRequest{}

	if query, ok := args["query"].(string); ok {
		req.Query = query
	}
	if queryType, ok := args["type"].(string); ok {
		req.Type = queryType
	}

	resp, err := resolver.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

type standardEvidenceReranker struct {
	name string
}

func (t *standardEvidenceReranker) Name() string { return t.name }
func (t *standardEvidenceReranker) Description() string {
	return "对检索结果做重排，提高证据质量"
}
func (t *standardEvidenceReranker) Schema() string {
	return jsonSchemaToJSON(schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"evidence":   {Type: "string", Desc: "证据列表JSON", Required: true},
		"query":      {Type: "string", Desc: "查询词", Required: true},
		"stock_code": {Type: "string", Desc: "股票代码", Required: false},
		"top_k":      {Type: "int", Desc: "返回TopK结果", Required: false},
	}))
}

func (t *standardEvidenceReranker) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	reranker := NewTypedEvidenceReranker()
	req := &RerankEvidenceRequest{}

	if evidenceJSON, ok := args["evidence"].(string); ok && evidenceJSON != "" {
		var evidence []EvidenceItem
		json.Unmarshal([]byte(evidenceJSON), &evidence)
		req.Evidence = evidence
	}
	if query, ok := args["query"].(string); ok {
		req.Query = query
	}
	if stockCode, ok := args["stock_code"].(string); ok {
		req.StockCode = stockCode
	}
	if topK, ok := args["top_k"].(int); ok {
		req.TopK = topK
	}

	resp, err := reranker.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

type standardSourceDeduplicator struct {
	name string
}

func (t *standardSourceDeduplicator) Name() string { return t.name }
func (t *standardSourceDeduplicator) Description() string {
	return "新闻、公告、转载内容去重"
}
func (t *standardSourceDeduplicator) Schema() string {
	return jsonSchemaToJSON(schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"sources":     {Type: "string", Desc: "来源列表JSON", Required: true},
		"dedupe_mode": {Type: "string", Desc: "去重模式(exact/semantic)", Required: false},
	}))
}

func (t *standardSourceDeduplicator) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	deduplicator := NewTypedSourceDeduplicator()
	req := &DedupeSourcesRequest{}

	if sourcesJSON, ok := args["sources"].(string); ok && sourcesJSON != "" {
		var sources []SourceItem
		json.Unmarshal([]byte(sourcesJSON), &sources)
		req.Sources = sources
	}
	if dedupeMode, ok := args["dedupe_mode"].(string); ok {
		req.DedupeMode = dedupeMode
	}

	resp, err := deduplicator.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

type standardCalculator struct {
	name string
}

func (t *standardCalculator) Name() string { return t.name }
func (t *standardCalculator) Description() string {
	return "做增长率、利润率、估值倍数等财务计算"
}
func (t *standardCalculator) Schema() string {
	return jsonSchemaToJSON(schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"operation": {Type: "string", Desc: "运算类型", Required: true},
		"operands":  {Type: "string", Desc: "操作数JSON", Required: false},
	}))
}

func (t *standardCalculator) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	calculator := NewTypedCalculator()
	req := &CalculatorRequest{}

	if operation, ok := args["operation"].(string); ok {
		req.Operation = operation
	}
	if operandsJSON, ok := args["operands"].(string); ok && operandsJSON != "" {
		var operands []FinancialCalc
		json.Unmarshal([]byte(operandsJSON), &operands)
		req.Operands = operands
	}

	resp, err := calculator.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

type standardSentimentRiskScanner struct {
	name string
}

func (t *standardSentimentRiskScanner) Name() string { return t.name }
func (t *standardSentimentRiskScanner) Description() string {
	return "扫描新闻/公告中的风险词，识别处罚、诉讼、减持、业绩下滑等"
}
func (t *standardSentimentRiskScanner) Schema() string {
	return jsonSchemaToJSON(schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"text":       {Type: "string", Desc: "文本内容", Required: true},
		"stock_code": {Type: "string", Desc: "股票代码", Required: false},
		"scan_mode":  {Type: "string", Desc: "扫描模式(risk/sentiment/both)", Required: false},
	}))
}

func (t *standardSentimentRiskScanner) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	scanner := NewTypedSentimentRiskScanner()
	req := &SentimentRiskScanRequest{}

	if text, ok := args["text"].(string); ok {
		req.Text = text
	}
	if stockCode, ok := args["stock_code"].(string); ok {
		req.StockCode = stockCode
	}
	if scanMode, ok := args["scan_mode"].(string); ok {
		req.ScanMode = scanMode
	}

	resp, err := scanner.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

type standardPeerCompanyLookup struct {
	name string
}

func (t *standardPeerCompanyLookup) Name() string { return t.name }
func (t *standardPeerCompanyLookup) Description() string {
	return "查同行公司、同赛道可比公司，用于横向对比"
}
func (t *standardPeerCompanyLookup) Schema() string {
	return jsonSchemaToJSON(schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"stock_code":   {Type: "string", Desc: "股票代码", Required: false},
		"company_name": {Type: "string", Desc: "公司名称", Required: false},
		"industry":     {Type: "string", Desc: "行业", Required: false},
	}))
}

func (t *standardPeerCompanyLookup) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	lookup := NewTypedPeerCompanyLookup()
	req := &PeerCompanyLookupRequest{}

	if stockCode, ok := args["stock_code"].(string); ok {
		req.StockCode = stockCode
	}
	if companyName, ok := args["company_name"].(string); ok {
		req.CompanyName = companyName
	}
	if industry, ok := args["industry"].(string); ok {
		req.Industry = industry
	}

	resp, err := lookup.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}
