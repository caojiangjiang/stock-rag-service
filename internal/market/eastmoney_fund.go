package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var eastMoneyFundInfoURL = "https://fundmobapi.eastmoney.com/FundMNewApi/FundMNFInfo"

type eastMoneyFundClient struct {
	client *http.Client
}

func newEastMoneyFundClient() *eastMoneyFundClient {
	return &eastMoneyFundClient{
		client: &http.Client{Timeout: 12 * time.Second},
	}
}

type eastMoneyFundResp struct {
	Datas []struct {
		FCODE      string  `json:"FCODE"`
		SHORTNAME  string  `json:"SHORTNAME"`
		PDATE      string  `json:"PDATE"`
		NAV        string  `json:"NAV"`
		ACCNAV     string  `json:"ACCNAV"`
		NAVCHGRT   string  `json:"NAVCHGRT"`
	} `json:"Datas"`
	ErrCode int    `json:"ErrCode"`
	Success bool   `json:"Success"`
	ErrMsg  string `json:"ErrMsg"`
}

// FetchFundNAVFromEastMoney 从东方财富/天天基金拉取最新公布单位净值。
func FetchFundNAVFromEastMoney(ctx context.Context, code string) (Quote, error) {
	code = normalizeFundCode(code)
	if code == "" {
		return Quote{}, fmt.Errorf("invalid fund code")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, eastMoneyFundInfoURL, nil)
	if err != nil {
		return Quote{}, err
	}
	q := req.URL.Query()
	q.Set("Fcodes", code)
	q.Set("plat", "Android")
	q.Set("appType", "ttjj")
	q.Set("product", "EFund")
	q.Set("Version", "1")
	q.Set("deviceid", "1")
	q.Set("pageIndex", "1")
	q.Set("pageSize", "1")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; StockRAG/1.0)")

	resp, err := newEastMoneyFundClient().client.Do(req)
	if err != nil {
		return Quote{}, fmt.Errorf("fetch fund nav: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Quote{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Quote{}, fmt.Errorf("fetch fund nav: status %d", resp.StatusCode)
	}

	var payload eastMoneyFundResp
	if err := json.Unmarshal(body, &payload); err != nil {
		return Quote{}, fmt.Errorf("parse fund nav: %w", err)
	}
	if payload.ErrCode != 0 || !payload.Success || len(payload.Datas) == 0 {
		msg := payload.ErrMsg
		if msg == "" {
			msg = "fund not found"
		}
		return Quote{}, fmt.Errorf("eastmoney: %s", msg)
	}

	row := payload.Datas[0]
	nav, err := strconv.ParseFloat(strings.TrimSpace(row.NAV), 64)
	if err != nil || nav <= 0 {
		return Quote{}, fmt.Errorf("invalid nav for %s", code)
	}
	chg, _ := strconv.ParseFloat(strings.TrimSpace(row.NAVCHGRT), 64)
	acc, _ := strconv.ParseFloat(strings.TrimSpace(row.ACCNAV), 64)

	return Quote{
		StockCode:     code,
		StockName:     strings.TrimSpace(row.SHORTNAME),
		Price:         nav,
		ChangePercent: chg,
		AccNAV:        acc,
		NavDate:       strings.TrimSpace(row.PDATE),
		HasQuote:      true,
		Source:        "eastmoney",
		AssetType:     "fund",
	}, nil
}

func normalizeFundCode(code string) string {
	code = strings.TrimSpace(code)
	if len(code) == 0 {
		return ""
	}
	// 天天基金代码一般为 6 位数字
	if len(code) < 6 && isDigits(code) {
		code = strings.Repeat("0", 6-len(code)) + code
	}
	return code
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isLikelyFundCode(code string) bool {
	code = normalizeFundCode(code)
	return len(code) == 6 && isDigits(code)
}
