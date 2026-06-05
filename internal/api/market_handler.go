package api

import (
	"net/http"
	"strings"

	"stock_rag/internal/market"
)

// MarketHandler 行情 API。
type MarketHandler struct{}

func NewMarketHandler() *MarketHandler {
	return &MarketHandler{}
}

func (h *MarketHandler) FundNAV(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		http.Error(w, "code is required", http.StatusBadRequest)
		return
	}
	q, err := market.LookupFundNAV(code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, q)
}

func (h *MarketHandler) StockQuote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		http.Error(w, "code is required", http.StatusBadRequest)
		return
	}
	mkt := strings.TrimSpace(r.URL.Query().Get("market"))
	q, err := market.LookupStockQuote(code, mkt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, q)
}
