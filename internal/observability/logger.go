package observability

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// StructuredLogEntry 结构化日志条目
type StructuredLogEntry struct {
	Timestamp    string                 `json:"timestamp"`
	Level        string                 `json:"level"`
	Service      string                 `json:"service"`
	TraceID      string                 `json:"trace_id,omitempty"`
	SpanID       string                 `json:"span_id,omitempty"`
	RequestID    string                 `json:"request_id,omitempty"`
	Message      string                 `json:"message"`
	Error        string                 `json:"error,omitempty"`
	LatencyMS    int64                  `json:"latency_ms,omitempty"`
	TokenIn      int                    `json:"token_in,omitempty"`
	TokenOut     int                    `json:"token_out,omitempty"`
	CostUSD      float64                `json:"cost_usd,omitempty"`
	ModelVersion string                 `json:"model_version,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// Logger 结构化日志记录器
type Logger struct {
	service string
	logger  *log.Logger
}

var (
	defaultLogger *Logger
	defaultOnce   sync.Once
	logOutput     io.Writer = os.Stdout
	logOutputOnce sync.Once
)

func initLogOutput() {
	logOutputOnce.Do(func() {
		path := os.Getenv("LOG_FILE")
		if path == "" {
			return
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			log.Printf("LOG_FILE: mkdir failed (%s): %v", path, err)
			return
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			log.Printf("LOG_FILE: open failed (%s): %v", path, err)
			return
		}
		logOutput = io.MultiWriter(os.Stdout, f)
		log.Printf("LOG_FILE: writing logs to %s (also stdout)", path)
	})
}

// L 返回全局默认 logger（service 取自 OTEL_SERVICE_NAME 或 fallback "stock_rag"）
func L() *Logger {
	defaultOnce.Do(func() {
		service := os.Getenv("OTEL_SERVICE_NAME")
		if service == "" {
			service = "stock_rag"
		}
		defaultLogger = NewLogger(service)
	})
	return defaultLogger
}

// NewLogger 创建结构化日志记录器
func NewLogger(service string) *Logger {
	initLogOutput()
	return &Logger{
		service: service,
		logger:  log.New(logOutput, "", 0),
	}
}

// Info 记录信息日志
func (l *Logger) Info(message string, fields ...interface{}) {
	l.log("INFO", message, fields...)
}

// Error 记录错误日志
func (l *Logger) Error(message string, err error, fields ...interface{}) {
	fields = append(fields, "error", err.Error())
	l.log("ERROR", message, fields...)
}

// Warn 记录警告日志
func (l *Logger) Warn(message string, fields ...interface{}) {
	l.log("WARN", message, fields...)
}

// Debug 记录调试日志
func (l *Logger) Debug(message string, fields ...interface{}) {
	l.log("DEBUG", message, fields...)
}

// InfoCtx / ErrorCtx / WarnCtx / DebugCtx 自动从 ctx 提取 OTel TraceID/SpanID
func (l *Logger) InfoCtx(ctx context.Context, message string, fields ...interface{}) {
	l.log("INFO", message, appendTraceFields(ctx, fields)...)
}

func (l *Logger) ErrorCtx(ctx context.Context, message string, err error, fields ...interface{}) {
	if err != nil {
		fields = append(fields, "error", err.Error())
	}
	l.log("ERROR", message, appendTraceFields(ctx, fields)...)
}

func (l *Logger) WarnCtx(ctx context.Context, message string, fields ...interface{}) {
	l.log("WARN", message, appendTraceFields(ctx, fields)...)
}

func (l *Logger) DebugCtx(ctx context.Context, message string, fields ...interface{}) {
	l.log("DEBUG", message, appendTraceFields(ctx, fields)...)
}

// appendTraceFields 从 ctx 的 OTel SpanContext 提取 trace_id/span_id 注入字段
func appendTraceFields(ctx context.Context, fields []interface{}) []interface{} {
	if ctx == nil {
		return fields
	}
	sc := trace.SpanContextFromContext(ctx)
	if sc.HasTraceID() {
		fields = append(fields, "trace_id", sc.TraceID().String())
	}
	if sc.HasSpanID() {
		fields = append(fields, "span_id", sc.SpanID().String())
	}
	return fields
}

// TraceIDFromContext 暴露给其他包用：从 ctx 拿 OTel TraceID（无则返回空字符串）
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	sc := trace.SpanContextFromContext(ctx)
	if sc.HasTraceID() {
		return sc.TraceID().String()
	}
	return ""
}

// log 通用日志记录方法
func (l *Logger) log(level, message string, fields ...interface{}) {
	entry := StructuredLogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     level,
		Service:   l.service,
		Message:   message,
	}

	// 解析字段
	metadata := make(map[string]interface{})
	for i := 0; i < len(fields); i += 2 {
		if i+1 >= len(fields) {
			break
		}
		key, ok := fields[i].(string)
		if !ok {
			continue
		}

		switch key {
		case "trace_id":
			entry.TraceID, _ = fields[i+1].(string)
		case "span_id":
			entry.SpanID, _ = fields[i+1].(string)
		case "request_id":
			entry.RequestID, _ = fields[i+1].(string)
		case "latency_ms":
			entry.LatencyMS, _ = fields[i+1].(int64)
		case "token_in":
			entry.TokenIn, _ = fields[i+1].(int)
		case "token_out":
			entry.TokenOut, _ = fields[i+1].(int)
		case "cost_usd":
			entry.CostUSD, _ = fields[i+1].(float64)
		case "model_version":
			entry.ModelVersion, _ = fields[i+1].(string)
		case "error":
			entry.Error, _ = fields[i+1].(string)
		default:
			metadata[key] = fields[i+1]
		}
	}

	if len(metadata) > 0 {
		entry.Metadata = metadata
	}

	// 序列化并输出
	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("Failed to marshal log entry: %v", err)
		return
	}

	l.logger.Println(string(data))
}
