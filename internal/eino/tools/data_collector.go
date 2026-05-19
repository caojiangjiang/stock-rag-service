package tools

import (
	"context"
	"fmt"
)

// StockData 股票数据结构
type StockData struct {
	BasicInfo     map[string]interface{}
	FinancialData map[string]interface{}
	NewsData      []string
}

// StockService 股票数据服务接口
type StockService interface {
	GetStockData(ctx context.Context, symbol string) (StockData, error)
}

// DataCollector 数据采集工具
type DataCollector struct {
	dataService StockService
}

// NewDataCollector 创建数据采集工具
func NewDataCollector(dataService StockService) *DataCollector {
	return &DataCollector{
		dataService: dataService,
	}
}

// Name 工具名称
func (t *DataCollector) Name() string { return "collect_stock_data" }

// Description 工具描述
func (t *DataCollector) Description() string {
	return "采集股票数据，包括基本信息、财务数据和新闻"
}

// Schema 工具参数schema
func (t *DataCollector) Schema() string {
	return `collect_stock_data(symbol)
  - symbol: 股票代码或名称（必填），支持参数名：symbol、stock_code、stock_name，如"600519"、"贵州茅台"
  示例：{"tool_name":"collect_stock_data","args":{"symbol":"600519"}}`
}

// Run 执行工具
func (t *DataCollector) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	// 支持多种参数名称
	var symbol string
	var ok bool

	// 按优先级查找参数
	if symbol, ok = args["symbol"].(string); ok {
	} else if symbol, ok = args["stock_code"].(string); ok {
	} else if symbol, ok = args["stock_name"].(string); ok {
	} else {
		return "", fmt.Errorf("缺少股票代码参数，请提供 symbol、stock_code 或 stock_name")
	}

	// 调用数据服务
	data, err := t.dataService.GetStockData(ctx, symbol)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("成功采集 %s 的数据：\n基本信息：%v\n财务数据：%v\n", symbol, data.BasicInfo, data.FinancialData), nil
}
