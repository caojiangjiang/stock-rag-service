package agent

import (
	"context"
	"fmt"
)

// StockAnalysisWorkflow 股票分析工作流
type StockAnalysisWorkflow struct {
	agent *Agent
}

// NewStockAnalysisWorkflow 创建股票分析工作流
func NewStockAnalysisWorkflow(agent *Agent) *StockAnalysisWorkflow {
	return &StockAnalysisWorkflow{
		agent: agent,
	}
}

// Run 执行工作流
func (w *StockAnalysisWorkflow) Run(ctx context.Context, symbol string) (string, error) {
	task := fmt.Sprintf(`
请对股票 %s 进行全面分析，包括：
1. 采集最新的股票数据
2. 分析财务状况
3. 评估投资前景
4. 生成详细分析报告

请按照上述步骤执行，并在每一步提供详细结果。
`, symbol)

	return w.agent.Run(ctx, task)
}
