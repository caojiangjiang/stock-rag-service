package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Pinger 健康检查接口
type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthResponse 是健康检查响应结构。
type HealthResponse struct {
	Status  string         `json:"status"`
	Service string         `json:"service"`
	Mode    string         `json:"mode,omitempty"`
	Details *HealthDetails `json:"details,omitempty"`
}

// HealthDetails 健康检查详情
type HealthDetails struct {
	Postgres string `json:"postgres"`
	Redis    string `json:"redis"`
	LLM      string `json:"llm"`
}

// ErrorResponse 是通用错误响应结构。
type ErrorResponse struct {
	Error string `json:"error"`
}

// LivenessResponse liveness 检查响应
type LivenessResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

// ReadinessResponse readiness 检查响应
type ReadinessResponse struct {
	Status   string          `json:"status"`
	Service  string          `json:"service"`
	Checks   map[string]bool `json:"checks"`
	Duration string          `json:"duration_ms,omitempty"`
}

// HealthDependencies 健康检查依赖
type HealthDependencies struct {
	PostgresDB  Pinger
	RedisClient *redis.Client
}

// NewHealthResponse 返回默认健康检查响应。
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

// LivenessHandler 返回 liveness 检查 handler（检查进程是否存活）
func LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		writeJSON(w, http.StatusOK, LivenessResponse{
			Status:  "ok",
			Service: "stock_rag",
		})
	}
}

// ReadinessHandler 返回 readiness 检查 handler（检查所有依赖是否就绪）
func ReadinessHandler(deps HealthDependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		start := time.Now()
		checks := make(map[string]bool)
		allHealthy := true

		// 检查 PostgreSQL
		if deps.PostgresDB != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := deps.PostgresDB.Ping(ctx); err != nil {
				checks["postgres"] = false
				allHealthy = false
			} else {
				checks["postgres"] = true
			}
		} else {
			checks["postgres"] = false
			allHealthy = false
		}

		// 检查 Redis
		if deps.RedisClient != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if _, err := deps.RedisClient.Ping(ctx).Result(); err != nil {
				checks["redis"] = false
				allHealthy = false
			} else {
				checks["redis"] = true
			}
		} else {
			checks["redis"] = false
			allHealthy = false
		}

		// 检查 LLM（通过环境变量判断）
		llmConfigured := strings.TrimSpace(os.Getenv("ARK_API_KEY")) != "" && strings.TrimSpace(os.Getenv("ARK_MODEL")) != ""
		checks["llm"] = llmConfigured
		if !llmConfigured {
			allHealthy = false
		}

		duration := time.Since(start).Milliseconds()

		response := ReadinessResponse{
			Status:   "ok",
			Service:  "stock_rag",
			Checks:   checks,
			Duration: formatDuration(duration),
		}

		if allHealthy {
			writeJSON(w, http.StatusOK, response)
		} else {
			response.Status = "degraded"
			writeJSON(w, http.StatusServiceUnavailable, response)
		}
	}
}

// formatDuration 格式化 duration 为字符串
func formatDuration(ms int64) string {
	return fmt.Sprintf("%dms", ms)
}

// HealthHandler 返回健康检查 handler（兼容旧版本）
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
