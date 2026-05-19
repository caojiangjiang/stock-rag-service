package observability

import (
	"encoding/json"
	"log"
	"os"
	"time"
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

// NewLogger 创建结构化日志记录器
func NewLogger(service string) *Logger {
	return &Logger{
		service: service,
		logger:  log.New(os.Stdout, "", 0),
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
