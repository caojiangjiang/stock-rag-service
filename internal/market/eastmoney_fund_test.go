package market

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchFundNAVFromEastMoney_Parse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Datas": []map[string]string{{
				"FCODE":     "003095",
				"SHORTNAME": "中欧医疗健康混合A",
				"PDATE":     "2026-06-04",
				"NAV":       "1.5450",
				"ACCNAV":    "1.7830",
				"NAVCHGRT":  "-0.34",
			}},
			"ErrCode": 0,
			"Success": true,
		})
	}))
	defer srv.Close()

	old := eastMoneyFundInfoURL
	eastMoneyFundInfoURL = srv.URL
	t.Cleanup(func() { eastMoneyFundInfoURL = old })

	q, err := FetchFundNAVFromEastMoney(context.Background(), "003095")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if q.Price != 1.5450 {
		t.Errorf("nav want 1.5450 got %v", q.Price)
	}
	if q.AccNAV != 1.7830 {
		t.Errorf("acc nav want 1.7830 got %v", q.AccNAV)
	}
	if q.NavDate != "2026-06-04" {
		t.Errorf("date want 2026-06-04 got %s", q.NavDate)
	}
	if q.Source != "eastmoney" {
		t.Errorf("source want eastmoney got %s", q.Source)
	}
}

func TestLiveProvider_GetFundNAV_Cache(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Datas": []map[string]string{{
				"FCODE": "003095", "SHORTNAME": "中欧医疗健康混合A",
				"PDATE": "2026-06-04", "NAV": "1.5450", "ACCNAV": "1.7830", "NAVCHGRT": "-0.34",
			}},
			"ErrCode": 0, "Success": true,
		})
	}))
	defer srv.Close()

	old := eastMoneyFundInfoURL
	eastMoneyFundInfoURL = srv.URL
	t.Cleanup(func() { eastMoneyFundInfoURL = old })

	p := NewLiveProvider()
	p.ttl = time.Minute
	p.fundTTL = time.Minute
	q1 := p.GetFundNAV("003095")
	q2 := p.GetFundNAV("003095")
	if !q1.HasQuote || !q2.HasQuote {
		t.Fatal("expected quotes")
	}
	if calls != 1 {
		t.Errorf("expected 1 upstream call, got %d", calls)
	}
}
