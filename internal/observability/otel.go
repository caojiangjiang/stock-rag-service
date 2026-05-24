package observability

import (
	"context"
	"encoding/base64"
	"log"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("stock_rag")

// InitTracer 初始化 OpenTelemetry tracer
// 通用 OTLP 导出器，兼容所有支持 OTLP 的系统（GrafanaCloud、Signoz、本地 OTel Collector 等）
func InitTracer(serviceName string) (func(context.Context) error, error) {
	// 创建资源
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		return nil, err
	}

	// 读取 OTLP 端点配置
	endpoint := os.Getenv("OTLP_ENDPOINT")
	if endpoint == "" {
		// 默认端点 - 只需要主机和端口，OTLP HTTP 客户端会自动添加 /v1/traces 路径
		endpoint = "localhost:4318"
	}
	// 容错：otlptracehttp.WithEndpoint 只接受 host:port，剥掉 scheme 和路径
	endpoint, insecure := normalizeOTLPEndpoint(endpoint)

	// 构建 OTLP HTTP 客户端
	httpOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(endpoint),
	}
	if insecure {
		httpOpts = append(httpOpts, otlptracehttp.WithInsecure())
	}

	// 如果配置了认证信息（GrafanaCloud 需要）
	user := os.Getenv("OTLP_USER")
	key := os.Getenv("OTLP_KEY")
	if user != "" && key != "" {
		httpOpts = append(httpOpts,
			otlptracehttp.WithHeaders(map[string]string{
				"Authorization": "Basic " + basicAuth(user, key),
			}),
		)
	}

	// 创建 OTLP 导出器
	client := otlptracehttp.NewClient(httpOpts...)
	exporter, err := otlptrace.New(context.Background(), client)
	if err != nil {
		return nil, err
	}

	log.Printf("Using OTLP exporter: %s", endpoint)

	// 创建 tracer provider
	tp := tracesdk.NewTracerProvider(
		tracesdk.WithBatcher(exporter),
		tracesdk.WithResource(res),
		tracesdk.WithSampler(tracesdk.AlwaysSample()),
	)

	// 设置全局 tracer provider
	otel.SetTracerProvider(tp)

	// 设置全局 propagator
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	log.Printf("OpenTelemetry tracer initialized for service: %s", serviceName)

	// 返回清理函数
	return func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return tp.Shutdown(ctx)
	}, nil
}

// basicAuth 生成 Basic Auth 字符串
func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

// normalizeOTLPEndpoint 把任意常见格式归一化为 host:port，并推断是否需要 insecure
// 支持：localhost:4318 / http://localhost:4318 / http://localhost:4318/v1/traces / https://host/otlp/v1/traces
func normalizeOTLPEndpoint(raw string) (string, bool) {
	insecure := true
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "https://") {
		s = strings.TrimPrefix(s, "https://")
		insecure = false
	} else if strings.HasPrefix(s, "http://") {
		s = strings.TrimPrefix(s, "http://")
	}
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	return s, insecure
}

// GetTracer 获取 tracer
func GetTracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// StartSpan 在调用链中开一个子 span，统一用 "stock_rag" tracer 名以便在 Jaeger 里聚合
// 用法：ctx, span := observability.StartSpan(ctx, "ChatService.Chat"); defer span.End()
func StartSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return otel.Tracer("stock_rag").Start(ctx, name)
}
