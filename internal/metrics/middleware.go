package metrics

import (
	"net/http"
	"strconv"
	"time"

	"stock_rag/internal/pkg/httpmiddleware"
)

// HTTPMetricsMiddleware HTTP 指标中间件（标准 net/http 版本）
func HTTPMetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 记录正在处理的请求数
		HTTPRequestsInFlight.Inc()
		defer HTTPRequestsInFlight.Dec()

		// 记录开始时间
		start := time.Now()

		// 创建响应写入器包装以捕获状态码，并保留 SSE 所需的 Flusher
		wrapped := &statusCodeWriter{ResponseWriter: httpmiddleware.PreserveFlusher(w), statusCode: http.StatusOK}

		// 处理请求
		next.ServeHTTP(wrapped, r)

		// 计算延迟
		duration := time.Since(start).Seconds()

		// 记录请求指标
		status := strconv.Itoa(wrapped.statusCode)
		path := r.URL.Path
		if path == "" {
			path = "/"
		}
		HTTPRequestsTotal.WithLabelValues(r.Method, path, status).Inc()
		HTTPRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
	})
}

// statusCodeWriter 包装 http.ResponseWriter 以捕获状态码
type statusCodeWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusCodeWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusCodeWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
