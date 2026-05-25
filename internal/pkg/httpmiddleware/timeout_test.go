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

	// http.TimeoutHandler 超时时返回 503 Service Unavailable
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
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
