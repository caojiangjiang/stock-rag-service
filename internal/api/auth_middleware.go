package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"stock_rag/internal/auth"
)

// ErrorResponse 通用错误响应。
type AuthErrorResponse struct {
	Error string `json:"error"`
}

func writeAuthError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(AuthErrorResponse{Error: msg})
}

// RequireAuth 强制 JWT 鉴权，将 user/session 写入 context。
func RequireAuth(authService auth.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := auth.ExtractBearerToken(r.Header.Get("Authorization"))
			if !ok || token == "" {
				writeAuthError(w, http.StatusUnauthorized, "缺少或无效的 Authorization")
				return
			}

			user, session, err := authService.ValidateToken(r.Context(), token)
			if err != nil {
				status := http.StatusUnauthorized
				msg := err.Error()
				if errors.Is(err, auth.ErrTokenRevoked) {
					msg = "token has been revoked"
				} else if errors.Is(err, auth.ErrInvalidToken) {
					msg = "token expired or invalid"
				}
				writeAuthError(w, status, msg)
				return
			}

			ctx := auth.ContextWithPrincipal(r.Context(), user, session)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole 在 RequireAuth 之后校验 RBAC 角色。
func RequireRole(roles ...auth.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, ok := auth.RoleFromContext(r.Context())
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, "未认证")
				return
			}
			if !auth.HasRole(role, roles...) {
				writeAuthError(w, http.StatusForbidden, "权限不足")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// OptionalAuth 有 token 则解析，无 token 则放行（用于逐步迁移）。
func OptionalAuth(authService auth.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := auth.ExtractBearerToken(r.Header.Get("Authorization"))
			if !ok || token == "" {
				next.ServeHTTP(w, r)
				return
			}
			user, session, err := authService.ValidateToken(r.Context(), token)
			if err == nil {
				ctx := auth.ContextWithPrincipal(r.Context(), user, session)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UserIDFromRequest 从 context 读取用户 ID（鉴权中间件设置）。
func UserIDFromRequest(ctx context.Context) string {
	if id, ok := auth.UserIDFromContext(ctx); ok {
		return id
	}
	return ""
}

// ProtectHandler 包装 HandlerFunc，强制鉴权。
func ProtectHandler(authService auth.AuthService, h http.HandlerFunc) http.HandlerFunc {
	protected := RequireAuth(authService)(h)
	return protected.ServeHTTP
}

// ProtectAdminHandler 包装 HandlerFunc，要求 admin 角色。
func ProtectAdminHandler(authService auth.AuthService, h http.HandlerFunc) http.HandlerFunc {
	protected := RequireAuth(authService)(RequireRole(auth.RoleAdmin)(h))
	return protected.ServeHTTP
}
