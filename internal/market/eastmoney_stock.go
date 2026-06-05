package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

var eastMoneyStockURL = "https://push2.eastmoney.com/api/qt/stock/get"

type eastMoneyStockResp struct {
	Data *struct {
		Code   string  `json:"f57"`
		Name   string  `json:"f58"`
		Price  float64 `json:"f43"`
		Change float64 `json:"f170"`
	} `json:"data"`
	RC int `json:"rc"`
}

// FetchStockQuoteFromEastMoney 拉取 A 股 / 美股 / 指数行情。
func FetchStockQuoteFromEastMoney(ctx context.Context, code, market string) (Quote, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return Quote{}, fmt.Errorf("empty code")
	}
	market = strings.ToLower(strings.TrimSpace(market))
	if market == "" {
		market = inferMarket(code)
	}

	var secids []string
	switch market {
	case "cn":
		secids = []string{CNStockSecID(code)}
	case "us":
		secids = USStockSecID(code)
	default:
		if sid := CNStockSecID(code); sid != "" && len(code) == 6 {
			secids = append(secids, sid)
		}
		secids = append(secids, USStockSecID(code)...)
	}

	var lastErr error
	for _, secid := range secids {
		if secid == "" {
			continue
		}
		q, err := fetchStockBySecID(ctx, secid, code)
		if err == nil {
			return q, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return Quote{}, lastErr
	}
	return Quote{}, fmt.Errorf("no secid for %s", code)
}

func fetchStockBySecID(ctx context.Context, secid, code string) (Quote, error) {
	u, err := url.Parse(eastMoneyStockURL)
	if err != nil {
		return Quote{}, err
	}
	q := u.Query()
	q.Set("secid", secid)
	q.Set("fields", "f43,f57,f58,f170")
	q.Set("fltt", "2")
	u.RawQuery = q.Encode()

	body, err := eastMoneyGET(ctx, u.String())
	if err != nil {
		return Quote{}, err
	}

	var payload eastMoneyStockResp
	if err := json.Unmarshal(body, &payload); err != nil {
		return Quote{}, err
	}
	if payload.RC != 0 || payload.Data == nil || payload.Data.Price <= 0 {
		return Quote{}, fmt.Errorf("quote unavailable for secid %s", secid)
	}

	symbol := payload.Data.Code
	if symbol == "" {
		symbol = code
	}

	return Quote{
		StockCode:     strings.ToUpper(symbol),
		StockName:     strings.TrimSpace(payload.Data.Name),
		Price:         payload.Data.Price,
		ChangePercent: payload.Data.Change,
		HasQuote:      true,
		Source:        "eastmoney",
		AssetType:     "stock",
	}, nil
}
