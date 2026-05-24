package observability

import (
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// statusRecorder 包装 ResponseWriter 以捕获状态码
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// TracingMiddleware 为每个 HTTP 请求创建 root span，注入 TraceID 到响应头与日志
// 上游若通过 W3C traceparent 透传，则自动续接父 span
func TracingMiddleware(serviceName string) func(http.Handler) http.Handler {
	tracer := otel.Tracer(serviceName)
	propagator := otel.GetTextMapPropagator()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 从请求头提取上游 trace context（W3C traceparent / baggage）
			ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			// 开 root span
			spanName := r.Method + " " + r.URL.Path
			ctx, span := tracer.Start(ctx, spanName,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					semconv.HTTPRequestMethodKey.String(r.Method),
					semconv.URLPath(r.URL.Path),
					semconv.URLScheme(schemeOf(r)),
					attribute.String("http.user_agent", r.UserAgent()),
				),
			)
			defer span.End()

			// 把 TraceID 写到响应头，方便前端/排障时一键定位
			traceID := span.SpanContext().TraceID().String()
			w.Header().Set("X-Trace-Id", traceID)

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, r.WithContext(ctx))
			latency := time.Since(start)

			span.SetAttributes(
				semconv.HTTPResponseStatusCode(rec.status),
				attribute.Int64("http.latency_ms", latency.Milliseconds()),
			)
			if rec.status >= 500 {
				span.SetStatus(codes.Error, http.StatusText(rec.status))
			}

			// 顺手写一条 access log（自动带 trace_id/span_id）
			L().InfoCtx(ctx, "http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"latency_ms", latency.Milliseconds(),
			)
		})
	}
}

func schemeOf(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
