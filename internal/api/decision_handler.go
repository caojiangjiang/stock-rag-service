package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"stock_rag/internal/decision"
)

// DecisionHandler 决策辅助 API。
type DecisionHandler struct {
	svc *decision.Service
}

func NewDecisionHandler(svc *decision.Service) *DecisionHandler {
	return &DecisionHandler{svc: svc}
}

func (h *DecisionHandler) DailyBrief(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := UserIDFromRequest(r.Context())
	if userID == "" {
		writeAuthError(w, http.StatusUnauthorized, "未认证")
		return
	}
	if h.svc == nil {
		http.Error(w, "decision service unavailable", http.StatusServiceUnavailable)
		return
	}
	forceRefresh := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("refresh")), "true") ||
		r.URL.Query().Get("refresh") == "1"

	var brief *decision.DailyBrief
	var err error
	if forceRefresh {
		brief, err = h.svc.RefreshDailyBrief(r.Context(), userID)
	} else {
		brief, err = h.svc.DailyBrief(r.Context(), userID)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, brief)
}

func (h *DecisionHandler) ExportHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := UserIDFromRequest(r.Context())
	if userID == "" {
		writeAuthError(w, http.StatusUnauthorized, "未认证")
		return
	}
	if h.svc == nil {
		http.Error(w, "decision service unavailable", http.StatusServiceUnavailable)
		return
	}
	filename := fmt.Sprintf("decision_history_%s.csv", time.Now().Format("20060102"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	if err := h.svc.ExportHistoryCSV(r.Context(), userID, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
