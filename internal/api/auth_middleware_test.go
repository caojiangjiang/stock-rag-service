package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"stock_rag/internal/auth"
)

func TestRequireAuth_Unauthorized(t *testing.T) {
	svc := auth.NewAuthServiceFromConfig(auth.AuthServiceConfig{
		UserStore:    auth.NewMemoryUserStore(),
		SessionStore: auth.NewMemorySessionStore(),
		JWTSecret:    "test-secret",
		AccessTTL:    time.Minute,
	})

	called := false
	handler := RequireAuth(svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
	if called {
		t.Fatal("handler should not run")
	}
}

func TestRequireRole_Forbidden(t *testing.T) {
	called := false
	handler := RequireRole(auth.RoleAdmin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(auth.ContextWithRole(context.Background(), auth.RoleUser))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("handler should not run")
	}
}
