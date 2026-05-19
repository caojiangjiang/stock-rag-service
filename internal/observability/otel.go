package observability

import (
	"context"
	"encoding/base64"
	"log"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/semconv/v1.24.0"
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
		// 默认端点
		endpoint = "http://localhost:4318/v1/traces"
	}

	// 构建 OTLP HTTP 客户端
	httpOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(), // 本地开发使用 insecure，生产环境建议移除
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

// GetTracer 获取 tracer
func GetTracer(name string) trace.Tracer {
	return otel.Tracer(name)
}
