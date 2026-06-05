package portfolio

import "time"

const (
	AssetStock = "stock"
	AssetFund  = "fund"
)

// Position 个人持仓（股票或基金）。
type Position struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	AssetType  string    `json:"asset_type"` // stock / fund
	StockCode  string    `json:"stock_code"`
	StockName  string    `json:"stock_name,omitempty"`
	Market     string    `json:"market"` // cn / us
	Quantity   float64   `json:"quantity"`
	CostPrice  float64   `json:"cost_price"`
	Currency   string    `json:"currency,omitempty"`
	Thesis          string    `json:"thesis,omitempty"`
	Falsification   string    `json:"falsification,omitempty"`
	OpenedAt        time.Time `json:"opened_at,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// UpsertRequest 创建或更新持仓。
type UpsertRequest struct {
	AssetType string  `json:"asset_type,omitempty"` // stock / fund，默认 stock
	StockCode string  `json:"stock_code"`
	StockName string  `json:"stock_name,omitempty"`
	Market    string  `json:"market,omitempty"`
	Quantity  float64 `json:"quantity"`
	CostPrice float64 `json:"cost_price"`
	Currency  string  `json:"currency,omitempty"`
	Thesis          string  `json:"thesis,omitempty"`
	Falsification   string  `json:"falsification,omitempty"`
}

// PositionView 带行情与盈亏的持仓视图。
type PositionView struct {
	Position
	CurrentPrice   float64 `json:"current_price"`
	ChangePercent  float64 `json:"change_percent"`
	MarketValue    float64 `json:"market_value"`
	CostValue      float64 `json:"cost_value"`
	UnrealizedPnL  float64 `json:"unrealized_pnl"`
	UnrealizedPct  float64 `json:"unrealized_pnl_pct"`
	HasQuote       bool    `json:"has_quote"`
	QuoteSource    string  `json:"quote_source,omitempty"` // eastmoney | mock | missing
	WeightPct      float64 `json:"weight_pct,omitempty"`
}

// Summary 持仓汇总。
type Summary struct {
	PositionCount int            `json:"position_count"`
	TotalCost     float64        `json:"total_cost"`
	TotalValue    float64        `json:"total_value"`
	UnrealizedPnL float64        `json:"unrealized_pnl"`
	UnrealizedPct float64        `json:"unrealized_pnl_pct"`
	Positions     []PositionView `json:"positions"`
	AsOf          time.Time      `json:"as_of"`
}

// FundAppImportRequest 从支付宝/天天基金等 App 截图字段导入基金。
// 只需填：代码、持有金额、持有收益、单位净值（可选，缺省走行情）。
type FundAppImportRequest struct {
	StockCode     string  `json:"stock_code"`
	StockName     string  `json:"stock_name,omitempty"`
	MarketValue   float64 `json:"market_value"`          // 持有金额
	HoldingPnL    float64 `json:"holding_pnl"`           // 持有收益（可负）
	UnitNAV       float64 `json:"unit_nav,omitempty"`    // 当前单位净值
	HoldingPnLPct float64 `json:"holding_pnl_pct,omitempty"` // 可选，用于校验
	Thesis        string  `json:"thesis,omitempty"`
}

// FundImportPreview 导入前计算预览。
type FundImportPreview struct {
	StockCode     string  `json:"stock_code"`
	StockName     string  `json:"stock_name,omitempty"`
	Quantity      float64 `json:"quantity"`
	CostPrice     float64 `json:"cost_price"`
	CostTotal     float64 `json:"cost_total"`
	MarketValue   float64 `json:"market_value"`
	HoldingPnL    float64 `json:"holding_pnl"`
	HoldingPnLPct float64 `json:"holding_pnl_pct"`
	UnitNAV       float64 `json:"unit_nav"`
}
