package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"stock_rag/internal/portfolio"
)

// PortfolioHandler 个人持仓 API。
type PortfolioHandler struct {
	svc      *portfolio.Service
	decision DecisionCacheInvalidator
}

// DecisionCacheInvalidator 持仓变更时使决策简报缓存失效。
type DecisionCacheInvalidator interface {
	InvalidateUserCache(userID string)
}

func NewPortfolioHandler(svc *portfolio.Service) *PortfolioHandler {
	return &PortfolioHandler{svc: svc}
}

func NewPortfolioHandlerWithDecision(svc *portfolio.Service, decision DecisionCacheInvalidator) *PortfolioHandler {
	return &PortfolioHandler{svc: svc, decision: decision}
}

func (h *PortfolioHandler) HandlePositions(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromRequest(r.Context())
	if userID == "" {
		writeAuthError(w, http.StatusUnauthorized, "未认证")
		return
	}
	if h.svc == nil {
		http.Error(w, "portfolio service unavailable", http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		positions, err := h.svc.List(r.Context(), userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, positions)
	case http.MethodPost:
		var req portfolio.UpsertRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		pos, err := h.svc.Upsert(r.Context(), userID, req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		h.invalidateDecision(userID)
		writeJSON(w, http.StatusOK, pos)
	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			http.Error(w, "id is required", http.StatusBadRequest)
			return
		}
		if err := h.svc.Delete(r.Context(), userID, id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		h.invalidateDecision(userID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *PortfolioHandler) Summary(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "portfolio service unavailable", http.StatusServiceUnavailable)
		return
	}
	summary, err := h.svc.Summary(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *PortfolioHandler) ImportFund(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := UserIDFromRequest(r.Context())
	if userID == "" {
		writeAuthError(w, http.StatusUnauthorized, "未认证")
		return
	}
	if h.svc == nil {
		http.Error(w, "portfolio service unavailable", http.StatusServiceUnavailable)
		return
	}

	var req portfolio.FundAppImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	previewOnly := strings.EqualFold(r.URL.Query().Get("preview"), "true")
	if previewOnly {
		preview, err := h.svc.PreviewFundImport(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, preview)
		return
	}

	pos, preview, err := h.svc.ImportFundFromApp(r.Context(), userID, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.invalidateDecision(userID)
	writeJSON(w, http.StatusOK, map[string]any{
		"position": pos,
		"preview":  preview,
	})
}

func (h *PortfolioHandler) invalidateDecision(userID string) {
	if h.decision != nil && userID != "" {
		h.decision.InvalidateUserCache(userID)
	}
}
