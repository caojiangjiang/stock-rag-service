package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

// HealthResponse 是健康检查响应结构。
type HealthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Mode    string `json:"mode"`
}

// ErrorResponse 是通用错误响应结构。
type ErrorResponse struct {
	Error string `json:"error"`
}

// NewHealthResponse 返回第一版默认健康检查响应。
func NewHealthResponse() HealthResponse {
	mode := "skeleton"
	if strings.TrimSpace(os.Getenv("ARK_API_KEY")) != "" && strings.TrimSpace(os.Getenv("ARK_MODEL")) != "" {
		mode = "ark"
	}

	return HealthResponse{
		Status:  "ok",
		Service: "stock_rag",
		Mode:    mode,
	}
}

// HealthHandler 返回健康检查 handler。
func HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		writeJSON(w, http.StatusOK, NewHealthResponse())
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
