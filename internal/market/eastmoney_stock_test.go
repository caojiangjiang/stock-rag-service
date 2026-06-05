package market

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchStockQuoteFromEastMoney(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rc": 0,
			"data": map[string]any{
				"f57": "600519", "f58": "贵州茅台", "f43": 1685.5, "f170": 1.25,
			},
		})
	}))
	defer srv.Close()

	old := eastMoneyStockURL
	eastMoneyStockURL = srv.URL
	t.Cleanup(func() { eastMoneyStockURL = old })

	q, err := FetchStockQuoteFromEastMoney(context.Background(), "600519", "cn")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if q.Price != 1685.5 {
		t.Errorf("price want 1685.5 got %v", q.Price)
	}
	if q.Source != "eastmoney" {
		t.Errorf("source want eastmoney got %s", q.Source)
	}
}

func TestFetchBoardConstituents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rc": 0,
			"data": map[string]any{
				"total": 2,
				"diff": []map[string]any{
					{"f12": "300024", "f14": "机器人", "f3": 5.6},
					{"f12": "688169", "f14": "石头科技", "f3": 3.2},
				},
			},
		})
	}))
	defer srv.Close()

	old := eastMoneyBoardListURL
	eastMoneyBoardListURL = srv.URL
	t.Cleanup(func() { eastMoneyBoardListURL = old })

	members, err := FetchBoardConstituents(context.Background(), "BK1090", 10)
	if err != nil {
		t.Fatalf("fetch board: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("want 2 members got %d", len(members))
	}
}

func TestLiveProvider_GetQuote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rc": 0,
			"data": map[string]any{"f57": "NVDA", "f58": "英伟达", "f43": 875.2, "f170": 2.8},
		})
	}))
	defer srv.Close()

	old := eastMoneyStockURL
	eastMoneyStockURL = srv.URL
	t.Cleanup(func() { eastMoneyStockURL = old })

	p := NewLiveProvider()
	q := p.GetQuote("NVDA")
	if !q.HasQuote || q.Price != 875.2 {
		t.Fatalf("unexpected quote: %+v", q)
	}
}
