package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"stock_rag/internal/model"

	"github.com/cloudwego/eino/schema"
)

type TypedTool[Req any, Rsp any] interface {
	Name() string
	Description() string
	Schema() *schema.ToolInfo
	Run(ctx context.Context, req *Req) (*Rsp, error)
}

type TypedToolBase interface {
	Name() string
	Description() string
	GetSchema() *schema.ToolInfo
	Invoke(ctx context.Context, args map[string]interface{}) (string, error)
}

type BaseTypedTool struct {
	name        string
	description string
}

func (b *BaseTypedTool) Name() string        { return b.name }
func (b *BaseTypedTool) Description() string { return b.description }

func (b *BaseTypedTool) GetSchema() *schema.ToolInfo {
	return nil
}

func (b *BaseTypedTool) Invoke(ctx context.Context, args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("Invoke not implemented for BaseTypedTool")
}

type RetrieveEvidenceRequest struct {
	Query     string `json:"query"`
	StockCode string `json:"stock_code"`
	TimeRange string `json:"time_range"`
	TopK      int    `json:"top_k"`
}

type RetrieveEvidenceResponse struct {
	Query         string              `json:"query"`
	StockCode     string              `json:"stock_code"`
	TotalCount    int                 `json:"total_count"`
	Indicators    []EvidenceIndicator `json:"indicators"`
	SourceCount   int                 `json:"source_count"`
	Confidence    float64             `json:"confidence"`
	EvidenceCount int                 `json:"evidence_count"`
}

type EvidenceIndicator struct {
	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	Content     string                 `json:"content"`
	DocType     string                 `json:"doc_type"`
	SourceURL   string                 `json:"source_url"`
	PublishedAt string                 `json:"published_at"`
	Confidence  float64                `json:"confidence"`
	Quality     string                 `json:"quality"`
	StockCode   string                 `json:"stock_code"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

type TypedRetrieveEvidenceTool struct {
	*BaseTypedTool
	queryService QueryService
}

func NewTypedRetrieveEvidenceTool(queryService QueryService) *TypedRetrieveEvidenceTool {
	return &TypedRetrieveEvidenceTool{
		BaseTypedTool: &BaseTypedTool{
			name:        "retrieve_evidence",
			description: "负责检索、过滤文档，返回结构化证据集合",
		},
		queryService: queryService,
	}
}

func (t *TypedRetrieveEvidenceTool) Schema() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: t.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query":      {Type: "string", Desc: "查询任务", Required: true},
			"stock_code": {Type: "string", Desc: "股票代码", Required: false},
			"time_range": {Type: "string", Desc: "时间范围", Required: false},
			"top_k":      {Type: "int", Desc: "返回数量", Required: false},
		}),
	}
}

func (t *TypedRetrieveEvidenceTool) Run(ctx context.Context, req *RetrieveEvidenceRequest) (*RetrieveEvidenceResponse, error) {
	if req.TopK <= 0 {
		req.TopK = 10
	}
	if req.TimeRange == "" {
		req.TimeRange = "365d"
	}

	chunks, err := t.queryService.Query(ctx, model.RAGQueryRequest{
		Question:  req.Query,
		StockCode: req.StockCode,
		TopK:      req.TopK,
		TimeRange: req.TimeRange,
	})
	if err != nil {
		return nil, fmt.Errorf("检索失败: %w", err)
	}

	indicators := make([]EvidenceIndicator, 0, len(chunks.Citations))
	for i, chunk := range chunks.Citations {
		stockCode := extractStockCodeFromTypedContent(chunk.Content)
		indicators = append(indicators, EvidenceIndicator{
			ID:          fmt.Sprintf("chunk_%d", i),
			Title:       chunk.Title,
			Content:     chunk.Content,
			DocType:     chunk.DocType,
			SourceURL:   chunk.SourceURL,
			PublishedAt: chunk.Published,
			Confidence:  0.8,
			Quality:     "medium",
			StockCode:   stockCode,
		})
	}

	highQualityCount := 0
	for _, ind := range indicators {
		if ind.Quality == "high" || ind.Confidence > 0.85 {
			highQualityCount++
		}
	}
	confidence := 0.5 + float64(highQualityCount)*0.1
	if confidence > 0.95 {
		confidence = 0.95
	}

	return &RetrieveEvidenceResponse{
		Query:         req.Query,
		StockCode:     req.StockCode,
		TotalCount:    len(indicators),
		Indicators:    indicators,
		SourceCount:   len(indicators),
		Confidence:    confidence,
		EvidenceCount: len(indicators),
	}, nil
}

type ExtractMetricsRequest struct {
	Query       string              `json:"query"`
	StockCode   string              `json:"stock_code"`
	EvidenceSet []EvidenceIndicator `json:"evidence_set"`
}

type ExtractMetricsResponse struct {
	Query       string            `json:"query"`
	StockCode   string            `json:"stock_code"`
	IsAligned   bool              `json:"is_aligned"`
	Indicators  []MetricIndicator `json:"indicators"`
	SourceCount int               `json:"source_count"`
	Confidence  float64           `json:"confidence"`
}

type MetricIndicator struct {
	Name     string  `json:"name"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit"`
	Year     string  `json:"year"`
	Source   string  `json:"source"`
	Caliber  string  `json:"caliber"`
	CalcType string  `json:"calc_type"`
}

type TypedExtractMetricsTool struct {
	*BaseTypedTool
	queryService QueryService
}

func NewTypedExtractMetricsTool(queryService QueryService) *TypedExtractMetricsTool {
	return &TypedExtractMetricsTool{
		BaseTypedTool: &BaseTypedTool{
			name:        "extract_metrics",
			description: "从证据中提取财务指标，做年份对齐和口径统一",
		},
		queryService: queryService,
	}
}

func (t *TypedExtractMetricsTool) Schema() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: t.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query":        {Type: "string", Desc: "查询任务", Required: true},
			"stock_code":   {Type: "string", Desc: "股票代码", Required: false},
			"evidence_set": {Type: "object", Desc: "证据集合", Required: false},
		}),
	}
}

func (t *TypedExtractMetricsTool) Run(ctx context.Context, req *ExtractMetricsRequest) (*ExtractMetricsResponse, error) {
	var evidenceSet []EvidenceIndicator

	if len(req.EvidenceSet) > 0 {
		evidenceSet = req.EvidenceSet
	} else if t.queryService != nil {
		chunks, err := t.queryService.Query(ctx, model.RAGQueryRequest{
			Question:  req.Query,
			StockCode: req.StockCode,
			TopK:      10,
		})
		if err == nil && len(chunks.Citations) > 0 {
			evidenceSet = make([]EvidenceIndicator, 0, len(chunks.Citations))
			for i, chunk := range chunks.Citations {
				evidenceSet = append(evidenceSet, EvidenceIndicator{
					ID:          fmt.Sprintf("chunk_%d", i),
					Title:       chunk.Title,
					Content:     chunk.Content,
					DocType:     chunk.DocType,
					PublishedAt: chunk.Published,
				})
			}
		}
	}

	if len(evidenceSet) == 0 {
		return &ExtractMetricsResponse{
			Query:       req.Query,
			StockCode:   req.StockCode,
			IsAligned:   true,
			Indicators:  []MetricIndicator{},
			SourceCount: 0,
			Confidence:  0.0,
		}, nil
	}

	metricTable := extractTypedMetrics(evidenceSet, req.StockCode)

	confidence := 0.5 + float64(len(metricTable))*0.05
	if confidence > 0.9 {
		confidence = 0.9
	}

	return &ExtractMetricsResponse{
		Query:       req.Query,
		StockCode:   req.StockCode,
		IsAligned:   true,
		Indicators:  metricTable,
		SourceCount: len(metricTable),
		Confidence:  confidence,
	}, nil
}

type GenerateReportRequest struct {
	Query       string              `json:"query"`
	StockCode   string              `json:"stock_code"`
	EvidenceSet []EvidenceIndicator `json:"evidence_set"`
	MetricTable []MetricIndicator   `json:"metric_table"`
}

type GenerateReportResponse struct {
	Summary    string            `json:"summary"`
	Confidence float64           `json:"confidence"`
	StockCode  string            `json:"stock_code"`
	KeyMetrics []MetricIndicator `json:"key_metrics"`
	Citations  []ReportCitation  `json:"citations"`
}

type ReportCitation struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type TypedGenerateReportTool struct {
	*BaseTypedTool
	queryService QueryService
}

func NewTypedGenerateReportTool(queryService QueryService) *TypedGenerateReportTool {
	return &TypedGenerateReportTool{
		BaseTypedTool: &BaseTypedTool{
			name:        "generate_report",
			description: "基于证据和指标生成最终分析报告",
		},
		queryService: queryService,
	}
}

func (t *TypedGenerateReportTool) Schema() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: t.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query":        {Type: "string", Desc: "用户问题", Required: true},
			"stock_code":   {Type: "string", Desc: "股票代码", Required: false},
			"evidence_set": {Type: "object", Desc: "证据集合", Required: false},
			"metric_table": {Type: "object", Desc: "指标表格", Required: false},
		}),
	}
}

func (t *TypedGenerateReportTool) Run(ctx context.Context, req *GenerateReportRequest) (*GenerateReportResponse, error) {
	evidenceSet := req.EvidenceSet
	metricTable := req.MetricTable

	if len(evidenceSet) == 0 && t.queryService != nil {
		chunks, err := t.queryService.Query(ctx, model.RAGQueryRequest{
			Question:  req.Query,
			StockCode: req.StockCode,
			TopK:      10,
		})
		if err == nil && len(chunks.Citations) > 0 {
			evidenceSet = make([]EvidenceIndicator, 0, len(chunks.Citations))
			for i, chunk := range chunks.Citations {
				evidenceSet = append(evidenceSet, EvidenceIndicator{
					ID:          fmt.Sprintf("chunk_%d", i),
					Title:       chunk.Title,
					Content:     chunk.Content,
					DocType:     chunk.DocType,
					SourceURL:   chunk.SourceURL,
					PublishedAt: chunk.Published,
				})
			}
		}
	}

	if len(metricTable) == 0 && len(evidenceSet) > 0 {
		metricTable = extractTypedMetrics(evidenceSet, req.StockCode)
	}

	summary := buildTypedAnalysisReport(req.Query, req.StockCode, evidenceSet, metricTable)

	citations := make([]ReportCitation, 0, minInt(5, len(evidenceSet)))
	for i, ev := range evidenceSet[:minInt(5, len(evidenceSet))] {
		citations = append(citations, ReportCitation{
			ID:    fmt.Sprintf("ref_%d", i+1),
			Title: ev.Title,
			URL:   ev.SourceURL,
		})
	}

	highQualityCount := 0
	for _, ev := range evidenceSet {
		if ev.Quality == "high" || ev.Confidence > 0.85 {
			highQualityCount++
		}
	}
	confidence := 0.5 + float64(highQualityCount)*0.1
	if len(metricTable) > 0 {
		confidence += 0.15
	}
	if confidence > 0.95 {
		confidence = 0.95
	}

	keyMetrics := metricTable
	if len(keyMetrics) > 10 {
		keyMetrics = keyMetrics[:10]
	}

	return &GenerateReportResponse{
		Summary:    summary,
		Confidence: confidence,
		StockCode:  req.StockCode,
		KeyMetrics: keyMetrics,
		Citations:  citations,
	}, nil
}

func extractTypedMetrics(evidenceSet []EvidenceIndicator, stockCode string) []MetricIndicator {
	metrics := make([]MetricIndicator, 0)

	patterns := []struct {
		name     string
		pattern  string
		calcType string
	}{
		{"营业收入", `营业收入[：:]\s*([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`, "absolute"},
		{"归属于母公司所有者的净利润", `归属于母公司所有者的净利润[：:]\s*([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`, "absolute"},
		{"扣除非经常性损益的净利润", `扣除非经常性损益的净利润[：:]\s*([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`, "absolute"},
		{"经营活动产生的现金流量净额", `经营活动产生的现金流量净额[：:]\s*([\d,]+(?:\.\d+)?)\s*(亿元|万元|元)?`, "absolute"},
		{"毛利率", `毛利率[：:]\s*([\d.]+)%`, "percentage"},
		{"净利率", `净利率[：:]\s*([\d.]+)%`, "percentage"},
		{"ROE", `ROE[：:]\s*([\d.]+)%`, "percentage"},
		{"资产负债率", `资产负债率[：:]\s*([\d.]+)%`, "percentage"},
	}

	for _, ev := range evidenceSet {
		if ev.Quality == "low" {
			continue
		}

		year := extractYearFromTypedTitle(ev.Title)
		for _, p := range patterns {
			re := regexp.MustCompile(p.pattern)
			matches := re.FindAllStringSubmatch(ev.Content, -1)

			for _, match := range matches {
				if len(match) >= 2 {
					value, _ := strconv.ParseFloat(strings.ReplaceAll(match[1], ",", ""), 64)
					unit := "亿元"
					if len(match) >= 3 && match[2] != "" {
						unit = match[2]
					}

					value = convertTypedMetricValue(value, unit)

					metrics = append(metrics, MetricIndicator{
						Name:    p.name,
						Value:   value,
						Unit:    "亿元",
						Year:    year,
						Source:  ev.Title,
						Caliber: "合并报表",
					})
				}
			}
		}
	}

	return metrics
}

func extractYearFromTypedTitle(title string) string {
	re := regexp.MustCompile(`(\d{4})年`)
	match := re.FindStringSubmatch(title)
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

func convertTypedMetricValue(value float64, unit string) float64 {
	switch unit {
	case "万元":
		return value / 10000
	case "元":
		return value / 100000000
	default:
		return value
	}
}

func buildTypedAnalysisReport(query, stockCode string, evidenceSet []EvidenceIndicator, metricTable []MetricIndicator) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("**问题**: %s\n\n", query))

	if stockCode != "" {
		builder.WriteString(fmt.Sprintf("**股票代码**: %s\n\n", stockCode))
	}

	if len(evidenceSet) > 0 {
		builder.WriteString("**参考文档**:\n")
		for i, ev := range evidenceSet[:minInt(5, len(evidenceSet))] {
			builder.WriteString(fmt.Sprintf("[%d] %s (置信度: %.2f)\n",
				i+1, ev.Title, ev.Confidence))
		}
		builder.WriteString("\n")
	}

	if len(metricTable) > 0 {
		builder.WriteString("**核心财务指标**:\n")
		builder.WriteString("| 指标 | 数值(亿元) | 年份 | 口径 |\n")
		builder.WriteString("|------|------------|------|------|\n")

		metricsByYear := make(map[string][]MetricIndicator)
		for _, metric := range metricTable {
			year := metric.Year
			if year == "" {
				year = "未知"
			}
			metricsByYear[year] = append(metricsByYear[year], metric)
		}

		for year, metrics := range metricsByYear {
			for _, metric := range metrics {
				unit := "亿元"
				if metric.CalcType == "percentage" {
					unit = "%"
				}
				builder.WriteString(fmt.Sprintf("| %s | %.2f%s | %s | %s |\n",
					metric.Name, metric.Value, unit, year, metric.Caliber))
			}
		}
		builder.WriteString("\n")
	}

	builder.WriteString("**综合评价**:\n")
	if len(evidenceSet) > 0 {
		builder.WriteString("- 公司基本面整体健康\n")
		builder.WriteString("- 建议关注后续财报发布\n")
	}

	builder.WriteString("\n**风险提示**:\n")
	builder.WriteString("- 以上分析基于公开文档信息，仅供参考\n")
	builder.WriteString("- 数据可能存在延迟或遗漏，请以官方公告为准\n")
	builder.WriteString("- 投资有风险，决策需谨慎\n")

	return builder.String()
}

func extractStockCodeFromTypedContent(content string) string {
	re := regexp.MustCompile(`(?:股票代码|代码|code)[:：]?\s*(\d{6})`)
	match := re.FindStringSubmatch(content)
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

type ToolInputValidator struct {
	required []string
}

type toolInfoJSON struct {
	Name        string           `json:"name"`
	Desc        string           `json:"desc"`
	ParamsOneOf *paramsOneOfJSON `json:"params_one_of,omitempty"`
}

type paramsOneOfJSON struct {
	Params map[string]paramInfoJSON `json:"params"`
}

type paramInfoJSON struct {
	Type     string `json:"type"`
	Desc     string `json:"desc"`
	Required bool   `json:"required"`
}

func NewToolInputValidator(schema *schema.ToolInfo) *ToolInputValidator {
	validator := &ToolInputValidator{
		required: make([]string, 0),
	}

	if schema == nil {
		return validator
	}

	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		return validator
	}

	var info toolInfoJSON
	if err := json.Unmarshal(schemaJSON, &info); err != nil {
		return validator
	}

	if info.ParamsOneOf != nil && info.ParamsOneOf.Params != nil {
		for name, param := range info.ParamsOneOf.Params {
			if param.Required {
				validator.required = append(validator.required, name)
			}
		}
	}

	return validator
}

func (v *ToolInputValidator) Validate(args map[string]interface{}) error {
	for _, name := range v.required {
		if _, ok := args[name]; !ok {
			return fmt.Errorf("缺少必需参数: %s", name)
		}
	}
	return nil
}

func ValidateToolInput(schema *schema.ToolInfo, args map[string]interface{}) error {
	validator := NewToolInputValidator(schema)
	return validator.Validate(args)
}

type OutputContract struct {
	ToolName       string
	Description    string
	ResponseSchema *schema.ToolInfo
}

func (c *OutputContract) ValidateResponse(resp interface{}) error {
	if resp == nil {
		return fmt.Errorf("响应不能为空")
	}
	return nil
}

var EvidenceOutputContract = &OutputContract{
	ToolName:    "retrieve_evidence",
	Description: "证据检索工具输出契约",
	ResponseSchema: &schema.ToolInfo{
		Name: "retrieve_evidence",
		Desc: "返回结构化证据集合",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query":          {Type: "string", Desc: "查询任务", Required: true},
			"stock_code":     {Type: "string", Desc: "股票代码", Required: false},
			"total_count":    {Type: "int", Desc: "总数量", Required: true},
			"indicators":     {Type: "array", Desc: "证据指标列表", Required: true},
			"source_count":   {Type: "int", Desc: "来源数量", Required: true},
			"confidence":     {Type: "number", Desc: "置信度", Required: true},
			"evidence_count": {Type: "int", Desc: "证据数量", Required: true},
		}),
	},
}

var MetricsOutputContract = &OutputContract{
	ToolName:    "extract_metrics",
	Description: "指标提取工具输出契约",
	ResponseSchema: &schema.ToolInfo{
		Name: "extract_metrics",
		Desc: "返回结构化财务指标",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query":        {Type: "string", Desc: "查询任务", Required: true},
			"stock_code":   {Type: "string", Desc: "股票代码", Required: false},
			"is_aligned":   {Type: "bool", Desc: "是否对齐", Required: true},
			"indicators":   {Type: "array", Desc: "指标列表", Required: true},
			"source_count": {Type: "int", Desc: "来源数量", Required: true},
			"confidence":   {Type: "number", Desc: "置信度", Required: true},
		}),
	},
}

var ReportOutputContract = &OutputContract{
	ToolName:    "generate_report",
	Description: "报告生成工具输出契约",
	ResponseSchema: &schema.ToolInfo{
		Name: "generate_report",
		Desc: "返回分析报告",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"summary":     {Type: "string", Desc: "报告摘要", Required: true},
			"confidence":  {Type: "number", Desc: "置信度", Required: true},
			"stock_code":  {Type: "string", Desc: "股票代码", Required: false},
			"key_metrics": {Type: "array", Desc: "关键指标", Required: false},
			"citations":   {Type: "array", Desc: "引用来源", Required: false},
		}),
	},
}

func ValidateOutput(toolName string, output interface{}) error {
	var contract *OutputContract
	switch toolName {
	case "retrieve_evidence":
		contract = EvidenceOutputContract
	case "extract_metrics":
		contract = MetricsOutputContract
	case "generate_report":
		contract = ReportOutputContract
	default:
		return nil
	}
	return contract.ValidateResponse(output)
}

type ToolOutputRegistry struct {
	contracts map[string]*OutputContract
}

func NewToolOutputRegistry() *ToolOutputRegistry {
	return &ToolOutputRegistry{
		contracts: map[string]*OutputContract{
			"retrieve_evidence": EvidenceOutputContract,
			"extract_metrics":   MetricsOutputContract,
			"generate_report":   ReportOutputContract,
		},
	}
}

func (r *ToolOutputRegistry) GetContract(toolName string) (*OutputContract, error) {
	if contract, ok := r.contracts[toolName]; ok {
		return contract, nil
	}
	return nil, fmt.Errorf("未找到工具 %s 的输出契约", toolName)
}

func (r *ToolOutputRegistry) Validate(toolName string, output interface{}) error {
	contract, err := r.GetContract(toolName)
	if err != nil {
		return err
	}
	return contract.ValidateResponse(output)
}
