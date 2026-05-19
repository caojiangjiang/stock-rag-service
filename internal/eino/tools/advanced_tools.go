package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/cloudwego/eino/schema"
)

type RerankEvidenceRequest struct {
	Evidence  []EvidenceItem `json:"evidence"`
	Query     string         `json:"query"`
	StockCode string         `json:"stock_code,omitempty"`
	TopK      int            `json:"top_k,omitempty"`
}

type EvidenceItem struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Content   string  `json:"content"`
	Source    string  `json:"source,omitempty"`
	Score     float64 `json:"score,omitempty"`
	Quality   string  `json:"quality,omitempty"`
	Published string  `json:"published,omitempty"`
}

type RerankEvidenceResponse struct {
	RerankedEvidence []EvidenceItem `json:"reranked_evidence"`
	TotalCount       int            `json:"total_count"`
	Summary          string         `json:"summary"`
}

type TypedEvidenceReranker struct {
	*BaseTypedTool
}

func NewTypedEvidenceReranker() *TypedEvidenceReranker {
	return &TypedEvidenceReranker{
		BaseTypedTool: &BaseTypedTool{
			name:        "rerank_evidence",
			description: "对检索结果做重排，提高证据质量",
		},
	}
}

func (t *TypedEvidenceReranker) Schema() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: t.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"evidence":   {Type: "string", Desc: "证据列表JSON", Required: true},
			"query":      {Type: "string", Desc: "查询词", Required: true},
			"stock_code": {Type: "string", Desc: "股票代码", Required: false},
			"top_k":      {Type: "int", Desc: "返回TopK结果", Required: false},
		}),
	}
}

func (t *TypedEvidenceReranker) Run(ctx context.Context, req *RerankEvidenceRequest) (*RerankEvidenceResponse, error) {
	if len(req.Evidence) == 0 {
		return nil, fmt.Errorf("没有可重排的证据")
	}

	if req.TopK <= 0 {
		req.TopK = len(req.Evidence)
	}

	reranked := t.rerankEvidence(req)

	return &RerankEvidenceResponse{
		RerankedEvidence: reranked,
		TotalCount:       len(reranked),
		Summary:          t.generateRerankSummary(reranked),
	}, nil
}

func (t *TypedEvidenceReranker) Invoke(ctx context.Context, args map[string]interface{}) (string, error) {
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

	resp, err := t.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

func (t *TypedEvidenceReranker) rerankEvidence(req *RerankEvidenceRequest) []EvidenceItem {
	evidence := make([]EvidenceItem, len(req.Evidence))
	copy(evidence, req.Evidence)

	queryLower := strings.ToLower(req.Query)

	for i := range evidence {
		score := t.calculateRelevanceScore(&evidence[i], queryLower, req.StockCode)
		evidence[i].Score = score
	}

	sort.Slice(evidence, func(i, j int) bool {
		return evidence[i].Score > evidence[j].Score
	})

	if req.TopK < len(evidence) {
		evidence = evidence[:req.TopK]
	}

	return evidence
}

func (t *TypedEvidenceReranker) calculateRelevanceScore(item *EvidenceItem, query, stockCode string) float64 {
	score := 0.0

	titleLower := strings.ToLower(item.Title)
	contentLower := strings.ToLower(item.Content)

	titleMatch := t.countKeywordMatches(query, titleLower)
	contentMatch := t.countKeywordMatches(query, contentLower)
	score += float64(titleMatch) * 3.0
	score += float64(contentMatch) * 1.0

	if strings.Contains(contentLower, query) {
		score += 5.0
	}

	if strings.Contains(titleLower, query) {
		score += 10.0
	}

	switch item.Quality {
	case "high":
		score += 20.0
	case "medium":
		score += 10.0
	case "low":
		score -= 5.0
	}

	if stockCode != "" && strings.Contains(contentLower, stockCode) {
		score += 5.0
	}

	if item.Published != "" {
		daysSincePublished := t.calculateDaysSince(item.Published)
		if daysSincePublished <= 7 {
			score += 15.0
		} else if daysSincePublished <= 30 {
			score += 10.0
		} else if daysSincePublished <= 365 {
			score += 5.0
		}
	}

	switch item.Source {
	case "年报", "招股书", "财报":
		score += 8.0
	case "券商研报":
		score += 5.0
	case "新闻":
		score += 2.0
	}

	return score
}

func (t *TypedEvidenceReranker) countKeywordMatches(query, text string) int {
	keywords := strings.Fields(query)
	count := 0
	for _, keyword := range keywords {
		if len(keyword) >= 2 && strings.Contains(text, keyword) {
			count++
		}
	}
	return count
}

func (t *TypedEvidenceReranker) calculateDaysSince(dateStr string) int {
	re := regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})`)
	match := re.FindStringSubmatch(dateStr)
	if len(match) < 4 {
		return 999
	}

	year := 0
	month := 0
	day := 0
	fmt.Sscanf(match[1], "%d", &year)
	fmt.Sscanf(match[2], "%d", &month)
	fmt.Sscanf(match[3], "%d", &day)

	days := (2025 - year) * 365
	days += (1 - month) * 30
	days += (1 - day)

	return days
}

func (t *TypedEvidenceReranker) generateRerankSummary(evidence []EvidenceItem) string {
	if len(evidence) == 0 {
		return "没有可重排的证据"
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("重排完成，共 %d 条证据\n", len(evidence)))

	highQualityCount := 0
	for _, e := range evidence {
		if e.Quality == "high" {
			highQualityCount++
		}
	}

	builder.WriteString(fmt.Sprintf("高质量证据: %d 条 (%.1f%%)\n",
		highQualityCount, float64(highQualityCount)/float64(len(evidence))*100))

	if len(evidence) > 0 {
		builder.WriteString(fmt.Sprintf("Top1: %s (得分: %.2f)", evidence[0].Title, evidence[0].Score))
	}

	return builder.String()
}

type DedupeSourcesRequest struct {
	Sources    []SourceItem `json:"sources"`
	DedupeMode string       `json:"dedupe_mode,omitempty"`
}

type SourceItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	URL       string `json:"url,omitempty"`
	Source    string `json:"source,omitempty"`
	Published string `json:"published,omitempty"`
}

type DedupeSourcesResponse struct {
	DedupedSources []SourceItem `json:"deduped_sources"`
	RemovedCount   int          `json:"removed_count"`
	TotalCount     int          `json:"total_count"`
	Summary        string       `json:"summary"`
}

type TypedSourceDeduplicator struct {
	*BaseTypedTool
}

func NewTypedSourceDeduplicator() *TypedSourceDeduplicator {
	return &TypedSourceDeduplicator{
		BaseTypedTool: &BaseTypedTool{
			name:        "dedupe_sources",
			description: "新闻、公告、转载内容去重，避免重复来源",
		},
	}
}

func (t *TypedSourceDeduplicator) Schema() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: t.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"sources":     {Type: "string", Desc: "来源列表JSON", Required: true},
			"dedupe_mode": {Type: "string", Desc: "去重模式(exact/semantic)", Required: false},
		}),
	}
}

func (t *TypedSourceDeduplicator) Run(ctx context.Context, req *DedupeSourcesRequest) (*DedupeSourcesResponse, error) {
	if len(req.Sources) == 0 {
		return nil, fmt.Errorf("没有可去重的来源")
	}

	if req.DedupeMode == "" {
		req.DedupeMode = "exact"
	}

	deduped := t.dedupeSources(req)

	return &DedupeSourcesResponse{
		DedupedSources: deduped,
		RemovedCount:   len(req.Sources) - len(deduped),
		TotalCount:     len(req.Sources),
		Summary:        t.generateDedupeSummary(req.Sources, deduped),
	}, nil
}

func (t *TypedSourceDeduplicator) Invoke(ctx context.Context, args map[string]interface{}) (string, error) {
	req := &DedupeSourcesRequest{}

	if sourcesJSON, ok := args["sources"].(string); ok && sourcesJSON != "" {
		var sources []SourceItem
		json.Unmarshal([]byte(sourcesJSON), &sources)
		req.Sources = sources
	}
	if dedupeMode, ok := args["dedupe_mode"].(string); ok {
		req.DedupeMode = dedupeMode
	}

	resp, err := t.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

func (t *TypedSourceDeduplicator) dedupeSources(req *DedupeSourcesRequest) []SourceItem {
	seen := make(map[string]bool)
	seenContent := make(map[string]string)
	result := []SourceItem{}

	for _, source := range req.Sources {
		keep := true

		if req.DedupeMode == "exact" {
			if source.URL != "" && seen[source.URL] {
				keep = false
			} else if seen[source.Title] {
				keep = false
			}
		} else {
			normalized := t.normalizeText(source.Title + " " + source.Content)
			for normalizedContent := range seenContent {
				similarity := t.calculateSimilarity(normalized, normalizedContent)
				if similarity > 0.85 {
					keep = false
					break
				}
			}
		}

		if keep {
			if source.URL != "" {
				seen[source.URL] = true
			}
			seen[source.Title] = true
			seenContent[t.normalizeText(source.Title+" ")] = source.Title
			result = append(result, source)
		}
	}

	return result
}

func (t *TypedSourceDeduplicator) normalizeText(text string) string {
	text = strings.ToLower(text)
	text = strings.ReplaceAll(text, " ", "")
	text = strings.ReplaceAll(text, "\n", "")
	text = strings.ReplaceAll(text, "\r", "")
	text = strings.ReplaceAll(text, "，", "")
	text = strings.ReplaceAll(text, "。", "")
	text = strings.ReplaceAll(text, "！", "")
	text = strings.ReplaceAll(text, "？", "")
	return text
}

func (t *TypedSourceDeduplicator) calculateSimilarity(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}

	if len(s1) == 0 || len(s2) == 0 {
		return 0.0
	}

	commonChars := 0
	seenChars := make(map[rune]bool)

	for _, c := range s1 {
		seenChars[c] = true
	}

	for _, c := range s2 {
		if seenChars[c] {
			commonChars++
		}
	}

	return float64(commonChars*2) / float64(len(s1)+len(s2))
}

func (t *TypedSourceDeduplicator) generateDedupeSummary(original, deduped []SourceItem) string {
	removed := len(original) - len(deduped)
	return fmt.Sprintf("去重完成: 原始 %d 条 -> 去重后 %d 条 (移除 %d 条重复)", len(original), len(deduped), removed)
}

type CalculatorRequest struct {
	Operation string          `json:"operation"`
	Operands  []FinancialCalc `json:"operands"`
}

type FinancialCalc struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit,omitempty"`
	Year  string  `json:"year,omitempty"`
}

type CalculatorResponse struct {
	Operation string       `json:"operation"`
	Results   []CalcResult `json:"results"`
	Summary   string       `json:"summary"`
}

type CalcResult struct {
	Name      string  `json:"name"`
	Value     float64 `json:"value"`
	Unit      string  `json:"unit,omitempty"`
	Formula   string  `json:"formula,omitempty"`
	Formatted string  `json:"formatted,omitempty"`
}

type TypedCalculator struct {
	*BaseTypedTool
}

func NewTypedCalculator() *TypedCalculator {
	return &TypedCalculator{
		BaseTypedTool: &BaseTypedTool{
			name:        "calculator",
			description: "做增长率、利润率、估值倍数等财务计算",
		},
	}
}

func (t *TypedCalculator) Schema() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: t.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"operation": {Type: "string", Desc: "运算类型", Required: true},
			"operands":  {Type: "string", Desc: "操作数JSON", Required: false},
		}),
	}
}

func (t *TypedCalculator) Run(ctx context.Context, req *CalculatorRequest) (*CalculatorResponse, error) {
	results := t.calculate(req)

	return &CalculatorResponse{
		Operation: req.Operation,
		Results:   results,
		Summary:   t.generateCalcSummary(results),
	}, nil
}

func (t *TypedCalculator) Invoke(ctx context.Context, args map[string]interface{}) (string, error) {
	req := &CalculatorRequest{}

	if operation, ok := args["operation"].(string); ok {
		req.Operation = operation
	}
	if operandsJSON, ok := args["operands"].(string); ok && operandsJSON != "" {
		var operands []FinancialCalc
		json.Unmarshal([]byte(operandsJSON), &operands)
		req.Operands = operands
	}

	resp, err := t.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

func (t *TypedCalculator) calculate(req *CalculatorRequest) []CalcResult {
	switch req.Operation {
	case "growth_rate":
		return t.calcGrowthRate(req.Operands)
	case "profit_margin":
		return t.calcProfitMargin(req.Operands)
	case "pe_ratio":
		return t.calcPERatio(req.Operands)
	case "pb_ratio":
		return t.calcPBRatio(req.Operands)
	case "roe":
		return t.calcROE(req.Operands)
	case "gross_margin":
		return t.calcGrossMargin(req.Operands)
	case "net_margin":
		return t.calcNetMargin(req.Operands)
	case "yoy_change":
		return t.calcYoYChange(req.Operands)
	case "qoq_change":
		return t.calcQoQChange(req.Operands)
	default:
		return t.calcBasicOperations(req.Operation, req.Operands)
	}
}

func (t *TypedCalculator) calcGrowthRate(operands []FinancialCalc) []CalcResult {
	if len(operands) < 2 {
		return []CalcResult{{Name: "error", Value: 0, Formatted: "需要至少2个操作数"}}
	}

	current := operands[0].Value
	previous := operands[1].Value

	if previous == 0 {
		return []CalcResult{{Name: "增长率", Value: 0, Formatted: "基数不能为0"}}
	}

	growthRate := ((current - previous) / previous) * 100

	return []CalcResult{
		{
			Name:      "增长率",
			Value:     growthRate,
			Unit:      "%",
			Formula:   fmt.Sprintf("(%v - %v) / %v * 100", current, previous, previous),
			Formatted: fmt.Sprintf("%.2f%%", growthRate),
		},
		{
			Name:      "绝对增长",
			Value:     current - previous,
			Unit:      operands[0].Unit,
			Formula:   fmt.Sprintf("%v - %v", current, previous),
			Formatted: fmt.Sprintf("%.2f %s", current-previous, operands[0].Unit),
		},
	}
}

func (t *TypedCalculator) calcProfitMargin(operands []FinancialCalc) []CalcResult {
	if len(operands) < 2 {
		return []CalcResult{{Name: "error", Value: 0, Formatted: "需要净利润和营业收入"}}
	}

	netProfit := operands[0].Value
	revenue := operands[1].Value

	if revenue == 0 {
		return []CalcResult{{Name: "净利率", Value: 0, Formatted: "营业收入不能为0"}}
	}

	margin := (netProfit / revenue) * 100

	return []CalcResult{
		{
			Name:      "净利率",
			Value:     margin,
			Unit:      "%",
			Formula:   fmt.Sprintf("%v / %v * 100", netProfit, revenue),
			Formatted: fmt.Sprintf("%.2f%%", margin),
		},
	}
}

func (t *TypedCalculator) calcPERatio(operands []FinancialCalc) []CalcResult {
	if len(operands) < 2 {
		return []CalcResult{{Name: "error", Value: 0, Formatted: "需要股价和每股收益"}}
	}

	price := operands[0].Value
	eps := operands[1].Value

	if eps == 0 {
		return []CalcResult{{Name: "市盈率", Value: 0, Formatted: "每股收益不能为0"}}
	}

	pe := price / eps

	return []CalcResult{
		{
			Name:      "市盈率(PE)",
			Value:     pe,
			Unit:      "倍",
			Formula:   fmt.Sprintf("%v / %v", price, eps),
			Formatted: fmt.Sprintf("%.2f 倍", pe),
		},
	}
}

func (t *TypedCalculator) calcPBRatio(operands []FinancialCalc) []CalcResult {
	if len(operands) < 2 {
		return []CalcResult{{Name: "error", Value: 0, Formatted: "需要股价和每股净资产"}}
	}

	price := operands[0].Value
	bps := operands[1].Value

	if bps == 0 {
		return []CalcResult{{Name: "市净率", Value: 0, Formatted: "每股净资产不能为0"}}
	}

	pb := price / bps

	return []CalcResult{
		{
			Name:      "市净率(PB)",
			Value:     pb,
			Unit:      "倍",
			Formula:   fmt.Sprintf("%v / %v", price, bps),
			Formatted: fmt.Sprintf("%.2f 倍", pb),
		},
	}
}

func (t *TypedCalculator) calcROE(operands []FinancialCalc) []CalcResult {
	if len(operands) < 2 {
		return []CalcResult{{Name: "error", Value: 0, Formatted: "需要净利润和净资产"}}
	}

	netProfit := operands[0].Value
	equity := operands[1].Value

	if equity == 0 {
		return []CalcResult{{Name: "净资产收益率", Value: 0, Formatted: "净资产不能为0"}}
	}

	roe := (netProfit / equity) * 100

	return []CalcResult{
		{
			Name:      "净资产收益率(ROE)",
			Value:     roe,
			Unit:      "%",
			Formula:   fmt.Sprintf("%v / %v * 100", netProfit, equity),
			Formatted: fmt.Sprintf("%.2f%%", roe),
		},
	}
}

func (t *TypedCalculator) calcGrossMargin(operands []FinancialCalc) []CalcResult {
	if len(operands) < 2 {
		return []CalcResult{{Name: "error", Value: 0, Formatted: "需要毛利润和营业收入"}}
	}

	grossProfit := operands[0].Value
	revenue := operands[1].Value

	if revenue == 0 {
		return []CalcResult{{Name: "毛利率", Value: 0, Formatted: "营业收入不能为0"}}
	}

	margin := (grossProfit / revenue) * 100

	return []CalcResult{
		{
			Name:      "毛利率",
			Value:     margin,
			Unit:      "%",
			Formula:   fmt.Sprintf("%v / %v * 100", grossProfit, revenue),
			Formatted: fmt.Sprintf("%.2f%%", margin),
		},
	}
}

func (t *TypedCalculator) calcNetMargin(operands []FinancialCalc) []CalcResult {
	return t.calcProfitMargin(operands)
}

func (t *TypedCalculator) calcYoYChange(operands []FinancialCalc) []CalcResult {
	return t.calcGrowthRate(operands)
}

func (t *TypedCalculator) calcQoQChange(operands []FinancialCalc) []CalcResult {
	return t.calcGrowthRate(operands)
}

func (t *TypedCalculator) calcBasicOperations(op string, operands []FinancialCalc) []CalcResult {
	if len(operands) < 2 {
		return []CalcResult{{Name: "error", Value: 0, Formatted: "需要至少2个操作数"}}
	}

	result := operands[0].Value
	for i := 1; i < len(operands); i++ {
		switch op {
		case "add", "plus":
			result += operands[i].Value
		case "subtract", "minus":
			result -= operands[i].Value
		case "multiply", "times":
			result *= operands[i].Value
		case "divide":
			if operands[i].Value != 0 {
				result /= operands[i].Value
			}
		}
	}

	return []CalcResult{
		{
			Name:      "计算结果",
			Value:     result,
			Unit:      operands[0].Unit,
			Formatted: fmt.Sprintf("%.4f %s", result, operands[0].Unit),
		},
	}
}

func (t *TypedCalculator) generateCalcSummary(results []CalcResult) string {
	if len(results) == 0 {
		return "计算完成，无结果"
	}

	var builder strings.Builder
	builder.WriteString("计算结果:\n")
	for _, r := range results {
		if r.Formatted != "" && r.Formatted != "error" {
			builder.WriteString(fmt.Sprintf("- %s: %s\n", r.Name, r.Formatted))
		}
	}

	return builder.String()
}

type SentimentRiskScanRequest struct {
	Text      string `json:"text"`
	StockCode string `json:"stock_code,omitempty"`
	ScanMode  string `json:"scan_mode,omitempty"`
}

type SentimentRiskScanResponse struct {
	Sentiment      string          `json:"sentiment"`
	RiskLevel      string          `json:"risk_level"`
	RiskScore      float64         `json:"risk_score"`
	RiskTerms      []RiskTerm      `json:"risk_terms"`
	SentimentTerms []SentimentTerm `json:"sentiment_terms"`
	Summary        string          `json:"summary"`
}

type RiskTerm struct {
	Term    string  `json:"term"`
	Weight  float64 `json:"weight"`
	Context string  `json:"context,omitempty"`
}

type SentimentTerm struct {
	Term    string  `json:"term"`
	Weight  float64 `json:"weight"`
	Context string  `json:"context,omitempty"`
}

type TypedSentimentRiskScanner struct {
	*BaseTypedTool
}

func NewTypedSentimentRiskScanner() *TypedSentimentRiskScanner {
	return &TypedSentimentRiskScanner{
		BaseTypedTool: &BaseTypedTool{
			name:        "sentiment_or_risk_scan",
			description: "扫描新闻/公告中的风险词，识别处罚、诉讼、减持、业绩下滑等",
		},
	}
}

func (t *TypedSentimentRiskScanner) Schema() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: t.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"text":       {Type: "string", Desc: "文本内容", Required: true},
			"stock_code": {Type: "string", Desc: "股票代码", Required: false},
			"scan_mode":  {Type: "string", Desc: "扫描模式(risk/sentiment/both)", Required: false},
		}),
	}
}

func (t *TypedSentimentRiskScanner) Run(ctx context.Context, req *SentimentRiskScanRequest) (*SentimentRiskScanResponse, error) {
	if req.Text == "" {
		return nil, fmt.Errorf("文本内容不能为空")
	}

	if req.ScanMode == "" {
		req.ScanMode = "both"
	}

	riskTerms, sentimentTerms, riskScore := t.scanText(req)

	sentiment := "neutral"
	if len(sentimentTerms) > len(riskTerms) {
		sentiment = "positive"
	} else if len(riskTerms) > len(sentimentTerms) {
		sentiment = "negative"
	}

	riskLevel := "low"
	if riskScore > 0.7 {
		riskLevel = "high"
	} else if riskScore > 0.4 {
		riskLevel = "medium"
	}

	return &SentimentRiskScanResponse{
		Sentiment:      sentiment,
		RiskLevel:      riskLevel,
		RiskScore:      riskScore,
		RiskTerms:      riskTerms,
		SentimentTerms: sentimentTerms,
		Summary:        t.generateRiskSummary(riskLevel, sentiment, riskScore),
	}, nil
}

func (t *TypedSentimentRiskScanner) Invoke(ctx context.Context, args map[string]interface{}) (string, error) {
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

	resp, err := t.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

func (t *TypedSentimentRiskScanner) scanText(req *SentimentRiskScanRequest) ([]RiskTerm, []SentimentTerm, float64) {
	riskTerms := []RiskTerm{}
	sentimentTerms := []SentimentTerm{}
	totalRiskScore := 0.0

	riskPatterns := map[string]float64{
		"亏损":   0.8,
		"下降":   0.6,
		"下滑":   0.7,
		"减少":   0.5,
		"减持":   0.7,
		"处罚":   0.9,
		"整改":   0.8,
		"调查":   0.9,
		"诉讼":   0.8,
		"仲裁":   0.7,
		"债务违约": 0.95,
		"业绩预减": 0.8,
		"业绩下滑": 0.85,
		"终止":   0.6,
		"取消":   0.5,
		"失败":   0.6,
		"风险":   0.5,
		"警告":   0.7,
		"警示函":  0.8,
		"立案":   0.9,
		"冻结":   0.85,
		"查封":   0.85,
		"降级":   0.7,
		"负面":   0.6,
	}

	positivePatterns := map[string]float64{
		"增长":   0.7,
		"上升":   0.6,
		"增加":   0.5,
		"增持":   0.8,
		"盈利":   0.7,
		"利润增长": 0.85,
		"业绩增长": 0.85,
		"突破":   0.6,
		"创新高":  0.8,
		"中标":   0.7,
		"签约":   0.5,
		"合作":   0.4,
		"获奖":   0.6,
		"荣誉":   0.5,
		"良好":   0.4,
		"稳健":   0.4,
		"积极":   0.5,
		"正面":   0.5,
		"超预期":  0.8,
		"大幅增长": 0.9,
	}

	textLower := strings.ToLower(req.Text)

	for term, weight := range riskPatterns {
		if strings.Contains(textLower, term) {
			riskTerms = append(riskTerms, RiskTerm{
				Term:   term,
				Weight: weight,
			})
			totalRiskScore += weight
		}
	}

	for term, weight := range positivePatterns {
		if strings.Contains(textLower, term) {
			sentimentTerms = append(sentimentTerms, SentimentTerm{
				Term:   term,
				Weight: weight,
			})
			totalRiskScore -= weight * 0.5
		}
	}

	maxPossibleScore := 10.0
	riskScore := math.Max(0, math.Min(1, totalRiskScore/maxPossibleScore))

	return riskTerms, sentimentTerms, riskScore
}

func (t *TypedSentimentRiskScanner) generateRiskSummary(riskLevel, sentiment string, riskScore float64) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("风险等级: %s (%.2f)\n", riskLevel, riskScore))
	builder.WriteString(fmt.Sprintf("情感倾向: %s\n", sentiment))

	return builder.String()
}

type PeerCompanyLookupRequest struct {
	StockCode   string `json:"stock_code,omitempty"`
	CompanyName string `json:"company_name,omitempty"`
	Industry    string `json:"industry,omitempty"`
}

type PeerCompanyLookupResponse struct {
	Company  string        `json:"company"`
	Industry string        `json:"industry"`
	Peers    []PeerCompany `json:"peers"`
	Summary  string        `json:"summary"`
}

type PeerCompany struct {
	StockCode string  `json:"stock_code"`
	StockName string  `json:"stock_name"`
	Exchange  string  `json:"exchange,omitempty"`
	MarketCap float64 `json:"market_cap,omitempty"`
	Reason    string  `json:"reason,omitempty"`
}

type TypedPeerCompanyLookup struct {
	*BaseTypedTool
}

func NewTypedPeerCompanyLookup() *TypedPeerCompanyLookup {
	return &TypedPeerCompanyLookup{
		BaseTypedTool: &BaseTypedTool{
			name:        "peer_company_lookup",
			description: "查同行公司、同赛道可比公司，用于横向对比",
		},
	}
}

func (t *TypedPeerCompanyLookup) Schema() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: t.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"stock_code":   {Type: "string", Desc: "股票代码", Required: false},
			"company_name": {Type: "string", Desc: "公司名称", Required: false},
			"industry":     {Type: "string", Desc: "行业", Required: false},
		}),
	}
}

func (t *TypedPeerCompanyLookup) Run(ctx context.Context, req *PeerCompanyLookupRequest) (*PeerCompanyLookupResponse, error) {
	company := req.CompanyName
	if company == "" && req.StockCode != "" {
		company = t.stockCodeToName(req.StockCode)
	}

	industry := req.Industry
	if industry == "" {
		industry = t.getIndustry(company)
	}

	peers := t.findPeers(company, industry)

	return &PeerCompanyLookupResponse{
		Company:  company,
		Industry: industry,
		Peers:    peers,
		Summary:  t.generatePeerSummary(company, peers),
	}, nil
}

func (t *TypedPeerCompanyLookup) Invoke(ctx context.Context, args map[string]interface{}) (string, error) {
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

	resp, err := t.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

func (t *TypedPeerCompanyLookup) stockCodeToName(code string) string {
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

func (t *TypedPeerCompanyLookup) getIndustry(company string) string {
	industries := map[string]string{
		"贵州茅台": "白酒",
		"比亚迪":  "新能源汽车",
		"宁德时代": "动力电池",
		"五粮液":  "白酒",
		"中国平安": "保险",
		"招商银行": "银行",
		"腾讯":   "互联网",
		"阿里巴巴": "互联网",
	}
	return industries[company]
}

func (t *TypedPeerCompanyLookup) findPeers(company, industry string) []PeerCompany {
	peerDB := map[string][]PeerCompany{
		"白酒": {
			{StockCode: "000858", StockName: "五粮液", Exchange: "SZ", Reason: "高端白酒"},
			{StockCode: "000568", StockName: "泸州老窖", Exchange: "SZ", Reason: "高端白酒"},
			{StockCode: "600779", StockName: "水井坊", Exchange: "SH", Reason: "次高端白酒"},
			{StockCode: "000596", StockName: "古井贡酒", Exchange: "SZ", Reason: "区域白酒龙头"},
		},
		"新能源汽车": {
			{StockCode: "002812", StockName: "恩捷股份", Exchange: "SZ", Reason: "锂电池隔膜"},
			{StockCode: "300014", StockName: "亿纬锂能", Exchange: "SZ", Reason: "动力电池"},
			{StockCode: "600733", StockName: "北汽蓝谷", Exchange: "SH", Reason: "新能源汽车"},
		},
		"动力电池": {
			{StockCode: "002594", StockName: "比亚迪", Exchange: "SZ", Reason: "动力电池+整车"},
			{StockCode: "300014", StockName: "亿纬锂能", Exchange: "SZ", Reason: "动力电池"},
			{StockCode: "002812", StockName: "恩捷股份", Exchange: "SZ", Reason: "锂电池材料"},
		},
		"银行": {
			{StockCode: "601318", StockName: "中国平安", Exchange: "SH", Reason: "综合性金融"},
			{StockCode: "600000", StockName: "浦发银行", Exchange: "SH", Reason: "股份制银行"},
			{StockCode: "601166", StockName: "兴业银行", Exchange: "SH", Reason: "股份制银行"},
		},
		"保险": {
			{StockCode: "601318", StockName: "中国平安", Exchange: "SH", Reason: "寿险+财险"},
			{StockCode: "601601", StockName: "中国太保", Exchange: "SH", Reason: "寿险+财险"},
			{StockCode: "601628", StockName: "中国人寿", Exchange: "SH", Reason: "寿险"},
		},
		"互联网": {
			{StockCode: "00700", StockName: "腾讯控股", Exchange: "HK", Reason: "社交+游戏"},
			{StockCode: "09988", StockName: "阿里巴巴", Exchange: "HK", Reason: "电商+云计算"},
			{StockCode: "JD", StockName: "京东", Exchange: "NASDAQ", Reason: "电商"},
		},
	}

	if peers, ok := peerDB[industry]; ok {
		var filtered []PeerCompany
		for _, p := range peers {
			if p.StockName != company {
				filtered = append(filtered, p)
			}
		}
		return filtered
	}

	return []PeerCompany{
		{StockCode: "N/A", StockName: "未找到可比公司", Reason: "行业信息不足"},
	}
}

func (t *TypedPeerCompanyLookup) generatePeerSummary(company string, peers []PeerCompany) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("%s 的同业公司:\n", company))

	for _, peer := range peers {
		builder.WriteString(fmt.Sprintf("- %s (%s): %s\n", peer.StockName, peer.StockCode, peer.Reason))
	}

	return builder.String()
}
