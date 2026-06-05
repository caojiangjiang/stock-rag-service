package market

import "strings"

// Quote 行情快照。
type Quote struct {
	StockCode     string  `json:"stock_code"`
	StockName     string  `json:"stock_name"`
	Price         float64 `json:"price"`
	ChangePercent float64 `json:"change_percent"`
	HasQuote      bool    `json:"has_quote"`
	NavDate       string  `json:"nav_date,omitempty"`
	AccNAV        float64 `json:"acc_nav,omitempty"`
	Source        string  `json:"source,omitempty"`
	AssetType     string  `json:"asset_type,omitempty"`
}

// Provider 行情提供者。
type Provider interface {
	GetQuote(code string) Quote
}

// FundNAVFetcher 公募基金净值。
type FundNAVFetcher interface {
	GetFundNAV(code string) Quote
}

// DefaultProvider 内置演示行情（与 financial_tools mock 对齐并扩展候选池）。
type DefaultProvider struct {
	quotes map[string]Quote
}

func NewDefaultProvider() *DefaultProvider {
	p := &DefaultProvider{quotes: map[string]Quote{}}
	for code, q := range seedQuotes() {
		q.StockCode = code
		q.HasQuote = true
		p.quotes[normalizeCode(code)] = q
	}
	return p
}

func (p *DefaultProvider) GetQuote(code string) Quote {
	key := normalizeCode(code)
	if q, ok := p.quotes[key]; ok {
		return q
	}
	return Quote{StockCode: code, HasQuote: false}
}

func normalizeCode(code string) string {
	return strings.TrimSpace(strings.ToUpper(code))
}

func seedQuotes() map[string]Quote {
	return map[string]Quote{
		"600519": {StockName: "贵州茅台", Price: 1685.50, ChangePercent: 1.25},
		"002594": {StockName: "比亚迪", Price: 238.60, ChangePercent: -0.85},
		"300750": {StockName: "宁德时代", Price: 182.30, ChangePercent: 2.15},
		"688256": {StockName: "寒武纪", Price: 198.40, ChangePercent: 4.82},
		"688041": {StockName: "海光信息", Price: 78.20, ChangePercent: 3.15},
		"002415": {StockName: "海康威视", Price: 32.50, ChangePercent: 1.90},
		"300033": {StockName: "同花顺", Price: 128.60, ChangePercent: 2.40},
		"300024": {StockName: "机器人", Price: 18.35, ChangePercent: 5.60},
		"688169": {StockName: "石头科技", Price: 285.00, ChangePercent: 3.20},
		"002371": {StockName: "北方华创", Price: 312.00, ChangePercent: 2.05},
		"000300": {StockName: "沪深300", Price: 3850.00, ChangePercent: 0.45},
		"NVDA":   {StockName: "NVIDIA", Price: 875.20, ChangePercent: 2.80},
		"MSFT":   {StockName: "Microsoft", Price: 415.30, ChangePercent: 1.10},
		"GOOGL":  {StockName: "Alphabet", Price: 175.40, ChangePercent: 0.95},
		"META":   {StockName: "Meta", Price: 505.20, ChangePercent: 1.65},
		"TSLA":   {StockName: "Tesla", Price: 248.50, ChangePercent: 3.40},
		"AMD":    {StockName: "AMD", Price: 162.30, ChangePercent: 2.20},
		"AAPL":   {StockName: "Apple", Price: 192.80, ChangePercent: 0.55},
		"AMZN":   {StockName: "Amazon", Price: 185.60, ChangePercent: 0.80},
		"AVGO":   {StockName: "Broadcom", Price: 1420.00, ChangePercent: 1.95},
		"SPY":    {StockName: "S&P 500 ETF", Price: 520.10, ChangePercent: 0.35},
		// 公募基金（净值演示）
		"005827": {StockName: "易方达蓝筹精选", Price: 2.1520, ChangePercent: 0.68},
		"110011": {StockName: "易方达中小盘", Price: 5.8420, ChangePercent: -0.35},
		"161725": {StockName: "招商中证白酒", Price: 0.8920, ChangePercent: 1.12},
		"519674": {StockName: "银河创新成长", Price: 6.2150, ChangePercent: 2.05},
		"003095": {StockName: "中欧医疗健康混合A", Price: 1.5450, ChangePercent: -0.34},
		"007119": {StockName: "睿远成长价值", Price: 1.6850, ChangePercent: 0.55},
		// 美股 ETF / 基金
		"VOO":  {StockName: "Vanguard S&P 500 ETF", Price: 478.20, ChangePercent: 0.32},
		"QQQ":  {StockName: "Invesco QQQ Trust", Price: 438.50, ChangePercent: 0.88},
		"VTI":  {StockName: "Vanguard Total Stock", Price: 265.40, ChangePercent: 0.28},
	}
}
