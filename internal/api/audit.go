package api

import (
	"context"
	"net/http"
	"strings"

	"stock_rag/internal/observability"
)

func auditEvent(ctx context.Context, action, result string, fields ...interface{}) {
	base := []interface{}{
		"audit", true,
		"action", action,
		"result", result,
	}
	observability.L().InfoCtx(ctx, "audit event", append(base, fields...)...)
}

func auditRequestEvent(r *http.Request, action, result string, fields ...interface{}) {
	if r == nil {
		auditEvent(context.Background(), action, result, fields...)
		return
	}
	base := []interface{}{
		"method", r.Method,
		"path", r.URL.Path,
		"remote_addr", clientIP(r),
	}
	auditEvent(r.Context(), action, result, append(base, fields...)...)
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	return r.RemoteAddr
}
