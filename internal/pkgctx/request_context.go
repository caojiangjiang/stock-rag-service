package pkgctx

import (
	"context"
	"sync"
)

type contextKey string

const (
	RequestIDKey   contextKey = "request_id"
	TraceIDKey     contextKey = "trace_id"
	SpanIDKey      contextKey = "span_id"
	UserIDKey      contextKey = "user_id"
	ExecutionCtxKey contextKey = "execution_ctx"
)

type RequestContext struct {
	RequestID  string
	TraceID    string
	SpanID     string
	UserID     string
	Attributes map[string]string
	mu         sync.RWMutex
}

func NewRequestContext(requestID string) *RequestContext {
	return &RequestContext{
		RequestID:  requestID,
		TraceID:    generateTraceID(requestID),
		SpanID:     generateSpanID(),
		Attributes: make(map[string]string),
	}
}

func (r *RequestContext) SetAttribute(key, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Attributes[key] = value
}

func (r *RequestContext) GetAttribute(key string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.Attributes[key]
}

func (r *RequestContext) SetUserID(userID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.UserID = userID
}

func WithRequestContext(ctx context.Context, rc *RequestContext) context.Context {
	return context.WithValue(ctx, RequestIDKey, rc.RequestID)
}

func GetRequestID(ctx context.Context) string {
	if reqID, ok := ctx.Value(RequestIDKey).(string); ok {
		return reqID
	}
	return ""
}

func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, TraceIDKey, traceID)
}

func GetTraceID(ctx context.Context) string {
	if traceID, ok := ctx.Value(TraceIDKey).(string); ok {
		return traceID
	}
	return ""
}

func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, UserIDKey, userID)
}

func GetUserID(ctx context.Context) string {
	if userID, ok := ctx.Value(UserIDKey).(string); ok {
		return userID
	}
	return ""
}

type TracingInfo struct {
	RequestID  string            `json:"request_id"`
	TraceID    string            `json:"trace_id"`
	SpanID     string            `json:"span_id"`
	UserID     string            `json:"user_id"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Timestamp  int64             `json:"timestamp"`
	Steps      []StepSpan        `json:"steps,omitempty"`
}

type StepSpan struct {
	SpanID     string `json:"span_id"`
	ParentSpan string `json:"parent_span,omitempty"`
	Name       string `json:"name"`
	StartMs    int64  `json:"start_ms"`
	EndMs      int64  `json:"end_ms"`
	DurationMs int64  `json:"duration_ms"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}

func (t *TracingInfo) AddStep(span StepSpan) {
	t.Steps = append(t.Steps, span)
}

func generateTraceID(requestID string) string {
	return requestID
}

// GenerateTraceID 生成一个全局唯一的 traceId
func GenerateTraceID() string {
	return randomString(32)
}

func generateSpanID() string {
	return randomString(16)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[i%len(letters)]
	}
	return string(b)
}

type MetricsCollector struct {
	RequestID     string
	StartTime     int64
	EndTime       int64
	StepLatencies []int64
	ToolMetrics   []ToolMetric
	TokenUsage    TokenUsage
	QueueWaitMs   int64
	HandoffCount  int
	RetryCount    int
}

type ToolMetric struct {
	ToolName   string `json:"tool_name"`
	Success    bool   `json:"success"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

func (m *MetricsCollector) AddStepLatency(latencyMs int64) {
	m.StepLatencies = append(m.StepLatencies, latencyMs)
}

func (m *MetricsCollector) AddToolMetric(metric ToolMetric) {
	m.ToolMetrics = append(m.ToolMetrics, metric)
}

func (m *MetricsCollector) RecordHandoff() {
	m.HandoffCount++
}

func (m *MetricsCollector) RecordRetry() {
	m.RetryCount++
}