package trace

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/callbacks"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"stock_rag/internal/observability"
)

// spanKey 用于在 context 中存储 span
type spanKey struct{}

// CreateAPMPlusCallback 创建APMPlus回调，为 Eino 组件创建 OTel span
func CreateAPMPlusCallback() callbacks.Handler {
	tracer := otel.Tracer("eino")

	return callbacks.NewHandlerBuilder().
		OnStartFn(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
			// 创建 OTel span
			spanName := fmt.Sprintf("eino.%s", info.Component)
			ctx, span := tracer.Start(ctx, spanName,
				trace.WithSpanKind(trace.SpanKindInternal),
				trace.WithAttributes(
					attribute.String("component", string(info.Component)),
					attribute.String("node_name", info.Name),
					attribute.String("node_type", info.Type),
				),
			)

			// 将 span 存入 context，以便后续回调使用
			ctx = context.WithValue(ctx, spanKey{}, span)

			// 记录日志（自动带 trace_id/span_id）
			observability.L().InfoCtx(ctx, "Eino component started",
				"component", string(info.Component),
				"node_name", info.Name,
				"node_type", info.Type,
			)

			return ctx
		}).
		OnEndFn(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
			// 从 context 获取 span 并结束
			if span, ok := ctx.Value(spanKey{}).(trace.Span); ok {
				observability.L().InfoCtx(ctx, "Eino component completed",
					"component", string(info.Component),
					"node_name", info.Name,
				)

				span.End()
			}
			return ctx
		}).
		OnErrorFn(func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
			// 从 context 获取 span，设置错误状态并结束
			if span, ok := ctx.Value(spanKey{}).(trace.Span); ok {
				span.SetStatus(codes.Error, err.Error())
				span.SetAttributes(
					attribute.String("error.message", err.Error()),
					attribute.String("component", string(info.Component)),
					attribute.String("node_name", info.Name),
				)

				observability.L().ErrorCtx(ctx, "Eino component error", err,
					"component", string(info.Component),
					"node_name", info.Name,
				)

				span.End()
			}
			return ctx
		}).
		Build()
}
