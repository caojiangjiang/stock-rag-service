package tools

import (
	"context"
	"fmt"
)

// FinancialAnalysis 财务分析结果
type FinancialAnalysis struct {
	Trends          map[string]interface{}
	KeyMetrics      map[string]interface{}
	Recommendations []string
}

// FinancialService 财务分析服务接口
type FinancialService interface {
	AnalyzeFinancials(ctx context.Context, symbol string) (FinancialAnalysis, error)
}

// FinancialAnalyzer 财务分析工具
type FinancialAnalyzer struct {
	analysisService FinancialService
}

// NewFinancialAnalyzer 创建财务分析工具
func NewFinancialAnalyzer(analysisService FinancialService) *FinancialAnalyzer {
	return &FinancialAnalyzer{
		analysisService: analysisService,
	}
}

// Name 工具名称
func (t *FinancialAnalyzer) Name() string { return "analyze_financials" }

// Description 工具描述
func (t *FinancialAnalyzer) Description() string {
	return "分析股票财务数据，包括趋势和指标"
}

// Run 执行工具
func (t *FinancialAnalyzer) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	symbol, ok := args["symbol"].(string)
	if !ok {
		return "", fmt.Errorf("缺少股票代码参数")
	}

	// 调用分析服务
	analysis, err := t.analysisService.AnalyzeFinancials(ctx, symbol)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("财务分析结果：\n趋势：%v\n关键指标：%v\n", analysis.Trends, analysis.KeyMetrics), nil
}
