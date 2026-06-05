package api

import (
	"net/http"
	"strings"

	"stock_rag/internal/theme"
)

// ThemeHandler 主题快照 API。
type ThemeHandler struct {
	svc *theme.Service
}

func NewThemeHandler(svc *theme.Service) *ThemeHandler {
	return &ThemeHandler{svc: svc}
}

func (h *ThemeHandler) Snapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.svc == nil {
		http.Error(w, "theme service unavailable", http.StatusServiceUnavailable)
		return
	}

	themeID := strings.TrimSpace(r.URL.Query().Get("theme"))
	market := strings.TrimSpace(r.URL.Query().Get("market"))

	resp, err := h.svc.Snapshot(r.Context(), themeID, market)
	if err != nil {
		if themeID != "" {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
