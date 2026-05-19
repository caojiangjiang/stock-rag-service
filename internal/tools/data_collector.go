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

// Run 执行工具
func (t *DataCollector) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	symbol, ok := args["symbol"].(string)
	if !ok {
		return "", fmt.Errorf("缺少股票代码参数")
	}

	// 调用数据服务
	data, err := t.dataService.GetStockData(ctx, symbol)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("成功采集 %s 的数据：\n基本信息：%v\n财务数据：%v\n", symbol, data.BasicInfo, data.FinancialData), nil
}
