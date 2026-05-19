package utils

import (
	"fmt"
	"log"
	"time"
)

// LogFields 定义日志字段
type LogFields struct {
	RequestID     string
	StockCode     string
	TopK          int
	Elapsed       time.Duration
	RetrievedCount int
	ToolName      string
	Step          int
	Success       bool
	Message       string
}

// Log 输出结构化日志
func Log(level string, message string, fields LogFields) {
	logMsg := fmt.Sprintf("%s %s", level, message)
	
	if fields.RequestID != "" {
		logMsg += fmt.Sprintf(" request_id=%s", fields.RequestID)
	}
	if fields.StockCode != "" {
		logMsg += fmt.Sprintf(" stock_code=%s", fields.StockCode)
	}
	if fields.TopK > 0 {
		logMsg += fmt.Sprintf(" top_k=%d", fields.TopK)
	}
	if fields.Elapsed > 0 {
		logMsg += fmt.Sprintf(" elapsed=%v", fields.Elapsed)
	}
	if fields.RetrievedCount >= 0 {
		logMsg += fmt.Sprintf(" retrieved_count=%d", fields.RetrievedCount)
	}
	if fields.ToolName != "" {
		logMsg += fmt.Sprintf(" tool_name=%s", fields.ToolName)
	}
	if fields.Step >= 0 {
		logMsg += fmt.Sprintf(" step=%d", fields.Step)
	}
	if fields.Success {
		logMsg += " success=true"
	} else {
		logMsg += " success=false"
	}
	if fields.Message != "" {
		logMsg += fmt.Sprintf(" message=%s", fields.Message)
	}

	log.Println(logMsg)
}

// Info 输出信息级别的结构化日志
func Info(message string, fields LogFields) {
	Log("INFO", message, fields)
}

// Error 输出错误级别的结构化日志
func Error(message string, fields LogFields) {
	Log("ERROR", message, fields)
}

// Warning 输出警告级别的结构化日志
func Warning(message string, fields LogFields) {
	Log("WARNING", message, fields)
}
