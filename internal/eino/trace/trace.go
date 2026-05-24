package trace

import (
	"context"
	"time"
)

// TraceEvent 表示一个trace事件
type TraceEvent struct {
	EventType  string                 `json:"event_type"`
	Component  string                 `json:"component"`
	Operation  string                 `json:"operation"`
	StartTime  time.Time              `json:"start_time"`
	EndTime    time.Time              `json:"end_time"`
	DurationMs int64                  `json:"duration_ms"`
	Success    bool                   `json:"success"`
	Error      string                 `json:"error,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// Tracer 定义trace接口
type Tracer interface {
	Start(context.Context, string, string, map[string]interface{}) (context.Context, func(bool, string))
	Record(context.Context, string, string, map[string]interface{})
	GetEvents() []TraceEvent
}

// DefaultTracer 实现默认的tracer
type DefaultTracer struct {
	events []TraceEvent
}

// NewDefaultTracer 创建一个新的默认tracer
func NewDefaultTracer() *DefaultTracer {
	return &DefaultTracer{
		events: make([]TraceEvent, 0),
	}
}

// Start 开始一个trace操作
func (t *DefaultTracer) Start(ctx context.Context, component, operation string, metadata map[string]interface{}) (context.Context, func(bool, string)) {
	startTime := time.Now()

	return ctx, func(success bool, errMsg string) {
		endTime := time.Now()
		durationMs := endTime.Sub(startTime).Milliseconds()

		event := TraceEvent{
			EventType:  "operation",
			Component:  component,
			Operation:  operation,
			StartTime:  startTime,
			EndTime:    endTime,
			DurationMs: durationMs,
			Success:    success,
			Metadata:   metadata,
		}

		if errMsg != "" {
			event.Error = errMsg
		}

		t.events = append(t.events, event)
	}
}

// Record 记录一个事件
func (t *DefaultTracer) Record(ctx context.Context, component, operation string, metadata map[string]interface{}) {
	event := TraceEvent{
		EventType:  "event",
		Component:  component,
		Operation:  operation,
		StartTime:  time.Now(),
		EndTime:    time.Now(),
		DurationMs: 0,
		Success:    true,
		Metadata:   metadata,
	}

	t.events = append(t.events, event)
}

// GetEvents 获取所有trace事件
func (t *DefaultTracer) GetEvents() []TraceEvent {
	return t.events
}

// Traceable 可被trace的接口
type Traceable interface {
	SetTracer(Tracer)
	GetTracer() Tracer
}
