package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"stock_rag/internal/memory"
	"stock_rag/internal/memory/long"
)

// MemoryHandler 用户画像与会话记忆 API。
type MemoryHandler struct {
	svc *memory.ProfileService
}

func NewMemoryHandler(svc *memory.ProfileService) *MemoryHandler {
	return &MemoryHandler{svc: svc}
}

func (h *MemoryHandler) Profile(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "memory service unavailable", http.StatusServiceUnavailable)
		return
	}
	profile, err := h.svc.GetProfile(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (h *MemoryHandler) Preferences(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := UserIDFromRequest(r.Context())
	if userID == "" {
		writeAuthError(w, http.StatusUnauthorized, "未认证")
		return
	}
	if h.svc == nil {
		http.Error(w, "memory service unavailable", http.StatusServiceUnavailable)
		return
	}
	var prefs long.UserPreferences
	if err := json.NewDecoder(r.Body).Decode(&prefs); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.svc.UpdatePreferences(r.Context(), userID, &prefs); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "unavailable") {
			status = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), status)
		return
	}
	profile, err := h.svc.GetProfile(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (h *MemoryHandler) DeleteInsight(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := UserIDFromRequest(r.Context())
	if userID == "" {
		writeAuthError(w, http.StatusUnauthorized, "未认证")
		return
	}
	insightID := strings.TrimSpace(r.URL.Query().Get("insight_id"))
	if insightID == "" {
		http.Error(w, "缺少 insight_id", http.StatusBadRequest)
		return
	}
	if h.svc == nil {
		http.Error(w, "memory service unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := h.svc.DeleteInsight(r.Context(), userID, insightID); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, long.ErrNotFound) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *MemoryHandler) Session(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := UserIDFromRequest(r.Context())
	if userID == "" {
		writeAuthError(w, http.StatusUnauthorized, "未认证")
		return
	}
	convID := strings.TrimSpace(r.URL.Query().Get("conversation_id"))
	if convID == "" {
		http.Error(w, "缺少 conversation_id", http.StatusBadRequest)
		return
	}
	if h.svc == nil {
		http.Error(w, "memory service unavailable", http.StatusServiceUnavailable)
		return
	}
	session, err := h.svc.GetSessionMemory(r.Context(), userID, convID)
	if err != nil {
		status := http.StatusInternalServerError
		if err.Error() == "forbidden" {
			status = http.StatusForbidden
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, http.StatusOK, session)
}
