package trace

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cloudwego/eino/callbacks"
)

// APMPlusTracer 实现APMPlus的tracer
type APMPlusTracer struct {
	tracer *DefaultTracer
	logger *BytePlusLogger
}

// NewAPMPlusTracer 创建一个新的APMPlus tracer
func NewAPMPlusTracer(logger *BytePlusLogger) *APMPlusTracer {
	return &APMPlusTracer{
		tracer: NewDefaultTracer(),
		logger: logger,
	}
}

// Start 开始一个trace操作
func (t *APMPlusTracer) Start(ctx context.Context, component, operation string, metadata map[string]interface{}) (context.Context, func(bool, string)) {
	// 添加traceId到metadata
	if metadata == nil {
		metadata = make(map[string]interface{})
	}

	// 生成或获取traceId
	traceId := getTraceId(ctx)
	if traceId == "" {
		traceId = generateTraceId()
		ctx = context.WithValue(ctx, "traceId", traceId)
	}
	metadata["traceId"] = traceId

	// 记录开始事件
	if t.logger != nil {
		t.logger.SendLog(ctx, "INFO", fmt.Sprintf("Component started: %s/%s", component, operation), map[string]string{
			"component": component,
			"operation": operation,
		})
	} else {
		log.Printf("[APMPlus TRACE] Component started, traceId: %s, component: %s, operation: %s", traceId, component, operation)
	}

	// 调用默认tracer的Start方法
	return t.tracer.Start(ctx, component, operation, metadata)
}

// Record 记录一个事件
func (t *APMPlusTracer) Record(ctx context.Context, component, operation string, metadata map[string]interface{}) {
	// 添加traceId到metadata
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	traceId := getTraceId(ctx)
	metadata["traceId"] = traceId

	// 记录事件
	if t.logger != nil {
		fields := make(map[string]string)
		fields["component"] = component
		fields["operation"] = operation
		for k, v := range metadata {
			fields[k] = fmt.Sprintf("%v", v)
		}
		t.logger.SendLog(ctx, "INFO", fmt.Sprintf("Record event: %s/%s", component, operation), fields)
	} else {
		log.Printf("[APMPlus TRACE] Record event, traceId: %s, component: %s, operation: %s", traceId, component, operation)
	}

	// 调用默认tracer的Record方法
	t.tracer.Record(ctx, component, operation, metadata)
}

// GetEvents 获取所有trace事件
func (t *APMPlusTracer) GetEvents() []TraceEvent {
	return t.tracer.GetEvents()
}

// getTraceId 从context中获取traceId
func getTraceId(ctx context.Context) string {
	if traceId, ok := ctx.Value("traceId").(string); ok {
		return traceId
	}
	return ""
}

// generateTraceId 生成一个traceId
func generateTraceId() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
}

// CreateAPMPlusCallback 创建APMPlus回调（使用火山引擎日志）
func CreateAPMPlusCallback(logger *BytePlusLogger) callbacks.Handler {
	return callbacks.NewHandlerBuilder().
		OnStartFn(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
			traceId := getTraceId(ctx)
			if traceId == "" {
				traceId = generateTraceId()
				ctx = context.WithValue(ctx, "traceId", traceId)
			}

			if logger != nil {
				logger.SendLog(ctx, "INFO", "Component started", map[string]string{
					"component": string(info.Component),
					"traceId":   traceId,
				})
			} else {
				log.Printf("[APMPlus TRACE] Component started, traceId: %s", traceId)
			}
			return ctx
		}).
		OnEndFn(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
			traceId := getTraceId(ctx)

			if logger != nil {
				logger.SendLog(ctx, "INFO", "Component completed", map[string]string{
					"component": string(info.Component),
					"traceId":   traceId,
				})
			} else {
				log.Printf("[APMPlus TRACE] Component completed, traceId: %s", traceId)
			}
			return ctx
		}).
		OnErrorFn(func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
			traceId := getTraceId(ctx)

			if logger != nil {
				logger.SendLog(ctx, "ERROR", fmt.Sprintf("Component error: %v", err), map[string]string{
					"component": string(info.Component),
					"traceId":   traceId,
					"error":     err.Error(),
				})
			} else {
				log.Printf("[APMPlus TRACE] Component error: %v, traceId: %s", err, traceId)
			}
			return ctx
		}).
		Build()
}
