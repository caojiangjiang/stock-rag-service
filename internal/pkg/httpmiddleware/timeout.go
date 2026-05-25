package httpmiddleware

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// Timeout 为下游 handler 附加请求级超时；超时后返回 504 与 JSON 错误体。
// 使用自定义实现而非 http.TimeoutHandler，以便 SSE 流式响应仍可 Flush。
func Timeout(timeout time.Duration) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			tw := &timeoutWriter{
				ResponseWriter: PreserveFlusher(w),
			}

			done := make(chan struct{})
			go func() {
				defer close(done)
				next(tw, r.WithContext(ctx))
			}()

			select {
			case <-done:
			case <-ctx.Done():
				tw.timeout()
			}
		}
	}
}

type timeoutWriter struct {
	http.ResponseWriter
	mu           sync.Mutex
	wroteHeader  bool
	timedOut     bool
}

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return
	}
	tw.wroteHeader = true
	tw.ResponseWriter.WriteHeader(code)
}

func (tw *timeoutWriter) Write(p []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return 0, http.ErrHandlerTimeout
	}
	if !tw.wroteHeader {
		tw.wroteHeader = true
	}
	return tw.ResponseWriter.Write(p)
}

func (tw *timeoutWriter) Flush() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return
	}
	if f, ok := tw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (tw *timeoutWriter) timeout() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.wroteHeader || tw.timedOut {
		return
	}
	tw.timedOut = true
	tw.ResponseWriter.Header().Set("Content-Type", "application/json")
	tw.ResponseWriter.WriteHeader(http.StatusGatewayTimeout)
	_, _ = tw.ResponseWriter.Write([]byte(`{"error":"request timeout"}`))
}
