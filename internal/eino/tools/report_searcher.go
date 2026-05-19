package tools

import (
	"context"
	"fmt"

	"stock_rag/internal/model"
)

// QueryService 定义查询服务接口
type QueryService interface {
	Query(ctx context.Context, req model.RAGQueryRequest) (model.RAGQueryResponse, error)
}

// ReportSearcher 年报搜索工具
type ReportSearcher struct {
	queryService QueryService
}

// NewReportSearcher 创建年报搜索工具
func NewReportSearcher(queryService QueryService) *ReportSearcher {
	return &ReportSearcher{
		queryService: queryService,
	}
}

// Name 工具名称
func (t *ReportSearcher) Name() string { return "search_reports" }

// Description 工具描述
func (t *ReportSearcher) Description() string {
	return "搜索年报信息，获取公司财务数据和业务信息"
}

// Schema 工具参数schema
func (t *ReportSearcher) Schema() string {
	return `search_reports(stock_code, question)
  - stock_code: 股票代码（必填），如"600519"
  - question: 查询问题（必填），如"贵州茅台2024年年度报告"
  示例：{"tool_name":"search_reports","args":{"stock_code":"600519","question":"贵州茅台2024年年度报告"}}`
}

// Run 执行工具
func (t *ReportSearcher) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	stockCode, ok := args["stock_code"].(string)
	if !ok {
		return "", fmt.Errorf("缺少股票代码参数")
	}

	question, ok := args["question"].(string)
	if !ok {
		return "", fmt.Errorf("缺少问题参数")
	}

	// 构建查询请求
	req := model.RAGQueryRequest{
		Question:  question,
		StockCode: stockCode,
		TopK:      5,
	}

	// 调用查询服务
	resp, err := t.queryService.Query(ctx, req)
	if err != nil {
		return "", err
	}

	// 构建返回结果
	result := fmt.Sprintf("搜索结果：\n答案：%s\n", resp.Answer)

	if len(resp.Citations) > 0 {
		result += "引用：\n"
		for i, citation := range resp.Citations {
			result += fmt.Sprintf("%d. %s (%s)\n", i+1, citation.Title, citation.Published)
		}
	}

	return result, nil
}

// CompareYearsTool 年份比较工具
type CompareYearsTool struct {
	queryService QueryService
}

// NewCompareYearsTool 创建年份比较工具
func NewCompareYearsTool(queryService QueryService) *CompareYearsTool {
	return &CompareYearsTool{
		queryService: queryService,
	}
}

// Name 工具名称
func (t *CompareYearsTool) Name() string { return "compare_years" }

// Description 工具描述
func (t *CompareYearsTool) Description() string {
	return "比较不同年份的财务数据和业务信息"
}

// Schema 工具参数schema
func (t *CompareYearsTool) Schema() string {
	return `compare_years(stock_code, question)
  - stock_code: 股票代码（必填），如"600519"
  - question: 比较问题（必填），如"贵州茅台2023年和2024年财务对比"
  示例：{"tool_name":"compare_years","args":{"stock_code":"600519","question":"贵州茅台2023年和2024年财务对比"}}`
}

// Run 执行工具
func (t *CompareYearsTool) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	stockCode, ok := args["stock_code"].(string)
	if !ok {
		return "", fmt.Errorf("缺少股票代码参数")
	}

	question, ok := args["question"].(string)
	if !ok {
		return "", fmt.Errorf("缺少问题参数")
	}

	// 构建查询请求
	req := model.RAGQueryRequest{
		Question:  question,
		StockCode: stockCode,
		TopK:      5,
	}

	// 调用查询服务
	resp, err := t.queryService.Query(ctx, req)
	if err != nil {
		return "", err
	}

	// 构建返回结果
	result := fmt.Sprintf("比较结果：\n答案：%s\n", resp.Answer)

	if len(resp.Citations) > 0 {
		result += "引用：\n"
		for i, citation := range resp.Citations {
			result += fmt.Sprintf("%d. %s (%s)\n", i+1, citation.Title, citation.Published)
		}
	}

	return result, nil
}

// ExtractFinancialMetricTool 财务指标提取工具
type ExtractFinancialMetricTool struct {
	queryService QueryService
}

// NewExtractFinancialMetricTool 创建财务指标提取工具
func NewExtractFinancialMetricTool(queryService QueryService) *ExtractFinancialMetricTool {
	return &ExtractFinancialMetricTool{
		queryService: queryService,
	}
}

// Name 工具名称
func (t *ExtractFinancialMetricTool) Name() string { return "extract_financial_metric" }

// Description 工具描述
func (t *ExtractFinancialMetricTool) Description() string {
	return "提取特定财务指标数据"
}

// Schema 工具参数schema
func (t *ExtractFinancialMetricTool) Schema() string {
	return `extract_financial_metric(stock_code, metric)
  - stock_code: 股票代码（必填），如"600519"
  - metric: 财务指标名称（必填），如"营业收入"、"净利润"、"总资产"、"净资产收益率"
  示例：{"tool_name":"extract_financial_metric","args":{"stock_code":"600519","metric":"营业收入"}}`
}

// Run 执行工具
func (t *ExtractFinancialMetricTool) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	stockCode, ok := args["stock_code"].(string)
	if !ok {
		return "", fmt.Errorf("缺少股票代码参数")
	}

	metric, ok := args["metric"].(string)
	if !ok {
		return "", fmt.Errorf("缺少财务指标参数")
	}

	// 构建查询请求
	req := model.RAGQueryRequest{
		Question:  fmt.Sprintf("%s的%s是多少？", stockCode, metric),
		StockCode: stockCode,
		TopK:      5,
	}

	// 调用查询服务
	resp, err := t.queryService.Query(ctx, req)
	if err != nil {
		return "", err
	}

	// 构建返回结果
	result := fmt.Sprintf("财务指标提取结果：\n答案：%s\n", resp.Answer)

	if len(resp.Citations) > 0 {
		result += "引用：\n"
		for i, citation := range resp.Citations {
			result += fmt.Sprintf("%d. %s (%s)\n", i+1, citation.Title, citation.Published)
		}
	}

	return result, nil
}
