package httpmiddleware

import (
	"net/http"
	"time"
)

// Timeout 为下游 handler 附加请求级超时；超时后返回 504 与 JSON 错误体。
func Timeout(timeout time.Duration) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		handler := http.TimeoutHandler(
			http.HandlerFunc(next),
			timeout,
			`{"error":"request timeout"}`,
		)
		return handler.ServeHTTP
	}
}
