package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	eastMoneyHTTPClient = &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        16,
			MaxIdleConnsPerHost: 8,
			IdleConnTimeout:     90 * time.Second,
		},
	}
	eastMoneyBatchURL = "https://push2.eastmoney.com/api/qt/ulist.np/get"
)

// SymbolRef 批量行情请求。
type SymbolRef struct {
	Code   string
	Market string
}

type eastMoneyUlistResp struct {
	Data *struct {
		Diff []struct {
			Code   string  `json:"f12"`
			Name   string  `json:"f14"`
			Price  float64 `json:"f2"`
			Change float64 `json:"f3"`
		} `json:"diff"`
	} `json:"data"`
	RC int `json:"rc"`
}

// BatchFetchStockQuotes 批量拉取行情，key 为 MARKET:CODE。
func BatchFetchStockQuotes(ctx context.Context, refs []SymbolRef) map[string]Quote {
	out := make(map[string]Quote, len(refs))
	if len(refs) == 0 {
		return out
	}

	secidToKey := make(map[string]string, len(refs))
	codeToKey := make(map[string]string, len(refs))
	var secids []string
	for _, ref := range refs {
		key := quoteMapKey(ref.Market, ref.Code)
		if _, ok := out[key]; ok {
			continue
		}
		codeToKey[strings.ToUpper(strings.TrimSpace(ref.Code))] = key
		sid := secidForRef(ref)
		if sid == "" {
			continue
		}
		if _, dup := secidToKey[sid]; dup {
			continue
		}
		secidToKey[sid] = key
		secids = append(secids, sid)
	}

	const chunk = 40
	for i := 0; i < len(secids); i += chunk {
		end := i + chunk
		if end > len(secids) {
			end = len(secids)
		}
		batch := secids[i:end]
		quotes, err := fetchUlistChunk(ctx, batch, codeToKey)
		if err != nil {
			continue
		}
		for k, q := range quotes {
			out[k] = q
		}
		if end < len(secids) {
			select {
			case <-ctx.Done():
				return out
			case <-time.After(180 * time.Millisecond):
			}
		}
	}
	return out
}

func fetchUlistChunk(ctx context.Context, secids []string, codeToKey map[string]string) (map[string]Quote, error) {
	u, err := url.Parse(eastMoneyBatchURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("fltt", "2")
	q.Set("fields", "f2,f3,f12,f14")
	q.Set("secids", strings.Join(secids, ","))
	u.RawQuery = q.Encode()

	body, err := eastMoneyGET(ctx, u.String())
	if err != nil {
		return nil, err
	}
	var payload eastMoneyUlistResp
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if payload.RC != 0 || payload.Data == nil {
		return nil, fmt.Errorf("ulist unavailable")
	}

	out := make(map[string]Quote, len(payload.Data.Diff))
	for _, row := range payload.Data.Diff {
		code := strings.TrimSpace(row.Code)
		if code == "" {
			continue
		}
		key := codeToKey[strings.ToUpper(code)]
		if key == "" {
			key = quoteMapKey(inferMarket(code), code)
		}
		if row.Price <= 0 {
			continue
		}
		out[key] = Quote{
			StockCode:     strings.ToUpper(code),
			StockName:     strings.TrimSpace(row.Name),
			Price:         row.Price,
			ChangePercent: row.Change,
			HasQuote:      true,
			Source:        "eastmoney",
			AssetType:     "stock",
		}
	}
	return out, nil
}

func secidForRef(ref SymbolRef) string {
	m := strings.ToLower(strings.TrimSpace(ref.Market))
	if m == "" {
		m = inferMarket(ref.Code)
	}
	switch m {
	case "cn":
		return CNStockSecID(ref.Code)
	case "us":
		for _, sid := range USStockSecID(ref.Code) {
			return sid
		}
	}
	return ""
}

func quoteMapKey(market, code string) string {
	return strings.ToUpper(strings.TrimSpace(market)) + ":" + strings.ToUpper(strings.TrimSpace(code))
}

// QuoteMapKey 导出给 theme 包使用。
func QuoteMapKey(market, code string) string {
	return quoteMapKey(market, code)
}

func eastMoneyGET(ctx context.Context, rawURL string) ([]byte, error) {
	eastMoneyGetMu.Lock()
	defer eastMoneyGetMu.Unlock()

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt*200) * time.Millisecond):
			}
		}
		body, err := doEastMoneyGET(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !isRetryableEastMoneyErr(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

func doEastMoneyGET(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://quote.eastmoney.com/")
	req.Header.Set("Accept", "application/json,text/plain,*/*")

	resp, err := eastMoneyHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("eastmoney status %d", resp.StatusCode)
	}
	return body, nil
}

func isRetryableEastMoneyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "eof") || strings.Contains(msg, "timeout") || strings.Contains(msg, "connection reset")
}

var eastMoneyGetMu sync.Mutex

// ThrottledEastMoneyGET 与 eastMoneyGET 相同（保留兼容）。
func ThrottledEastMoneyGET(ctx context.Context, rawURL string) ([]byte, error) {
	return eastMoneyGET(ctx, rawURL)
}
