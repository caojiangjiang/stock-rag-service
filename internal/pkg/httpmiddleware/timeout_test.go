package httpmiddleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTimeoutReturnsGatewayTimeout(t *testing.T) {
	handler := Timeout(20 * time.Millisecond)(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(100 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	// 超时且 handler 未写响应时返回 504 Gateway Timeout
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected %d, got %d", http.StatusGatewayTimeout, rec.Code)
	}
}

func TestTimeoutAllowsFastHandler(t *testing.T) {
	handler := Timeout(time.Second)(func(w http.ResponseWriter, r *http.Request) {
		if err := context.Cause(r.Context()); err != nil {
			t.Fatalf("unexpected context error: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}
}
