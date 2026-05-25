package httpmiddleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTimeoutWriterImplementsFlusher(t *testing.T) {
	rec := httptest.NewRecorder()
	tw := &timeoutWriter{ResponseWriter: PreserveFlusher(rec)}
	if _, ok := any(tw).(http.Flusher); !ok {
		t.Fatal("timeoutWriter should implement http.Flusher")
	}
}

func TestStatusStubImplementsFlusher(t *testing.T) {
	rec := httptest.NewRecorder()
	var w http.ResponseWriter = &statusStub{ResponseWriter: rec}
	if _, ok := w.(http.Flusher); !ok {
		t.Fatal("statusStub should implement Flusher")
	}
}

func TestPreserveFlusherThroughMiddlewareStack(t *testing.T) {
	var gotFlusher bool

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, gotFlusher = w.(http.Flusher)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/chat/stream", nil)
	rec := httptest.NewRecorder()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrapped := &statusStub{ResponseWriter: PreserveFlusher(w)}
		final.ServeHTTP(wrapped, r)
	})

	Timeout(120 * time.Second)(inner)(rec, req)

	if !gotFlusher {
		t.Fatal("expected http.Flusher through middleware stack")
	}
}

type statusStub struct {
	http.ResponseWriter
}

func (s *statusStub) WriteHeader(code int) {
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusStub) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
