package trace

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// BytePlusLogger 火山引擎日志客户端（简化版，不依赖官方SDK）
type BytePlusLogger struct {
	endpoint    string
	accessKey   string
	secretKey   string
	topicID     string
	enabled     bool
	localPrint  bool
	httpClient  *http.Client
	logFile     *os.File
	logFilePath string
	mu          sync.Mutex
}

// NewBytePlusLogger 创建火山引擎日志客户端
func NewBytePlusLogger(endpoint, accessKey, secretKey, topicID string) *BytePlusLogger {
	logger := &BytePlusLogger{
		endpoint:   endpoint,
		accessKey:  accessKey,
		secretKey:  secretKey,
		topicID:    topicID,
		enabled:    false,
		localPrint: false,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	if endpoint == "" || accessKey == "" || secretKey == "" || topicID == "" {
		log.Println("[BytePlusLogger] 未配置火山引擎日志参数，将仅输出到本地文件")
	} else {
		logger.enabled = true
		log.Println("[BytePlusLogger] 火山引擎日志已启用")
	}

	os.MkdirAll("logs", 0755)
	logFile, err := os.OpenFile("logs/app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("[BytePlusLogger] 打开日志文件失败: %v，将仅输出到控制台", err)
	} else {
		logger.logFile = logFile
		logger.logFilePath = "logs/app.log"
		log.Printf("[BytePlusLogger] 日志文件: %s", logger.logFilePath)
	}

	return logger
}

// Close 关闭日志文件
func (l *BytePlusLogger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.logFile != nil {
		l.logFile.Close()
	}
}

// LogEntry 日志条目
type LogEntry struct {
	Time     int64             `json:"time"`
	Contents []LogContentEntry `json:"contents"`
}

// LogContentEntry 日志内容
type LogContentEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// LogGroup 日志组
type LogGroup struct {
	Logs []LogEntry `json:"logs"`
}

// SendLog 发送日志到火山引擎
func (l *BytePlusLogger) SendLog(ctx context.Context, level, message string, fields map[string]string) {
	// 添加基础字段
	if fields == nil {
		fields = make(map[string]string)
	}
	fields["level"] = level
	fields["message"] = message
	fields["timestamp"] = time.Now().Format(time.RFC3339)
	fields["traceId"] = getTraceId(ctx)

	// 构建日志消息
	logMsg := fmt.Sprintf("[%s] %s | ", time.Now().Format("2006-01-02 15:04:05"), level)
	for k, v := range fields {
		logMsg += fmt.Sprintf("%s=%s ", k, v)
	}

	// 写入本地文件
	if l.logFile != nil {
		l.mu.Lock()
		l.logFile.WriteString(logMsg + "\n")
		l.mu.Unlock()
	}

	// 发送到火山引擎
	if l.enabled {
		go func() {
			err := l.sendToBytePlus(fields)
			if err != nil {
				log.Printf("[BytePlusLogger] 发送日志失败: %v", err)
			}
		}()
	}
}

// sendToBytePlus 发送日志到火山引擎 TLS
func (l *BytePlusLogger) sendToBytePlus(fields map[string]string) error {
	// 构建日志条目
	logEntry := LogEntry{
		Time: time.Now().Unix(),
	}
	for k, v := range fields {
		logEntry.Contents = append(logEntry.Contents, LogContentEntry{
			Key:   k,
			Value: v,
		})
	}

	logGroup := LogGroup{
		Logs: []LogEntry{logEntry},
	}

	// 序列化为 JSON
	jsonData, err := json.Marshal(logGroup)
	if err != nil {
		return fmt.Errorf("序列化日志失败: %v", err)
	}

	// 构建请求 URL
	url := fmt.Sprintf("https://%s/%s", l.endpoint, l.topicID)

	// 生成签名
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	signature := l.generateSignature("POST", url, string(jsonData), timestamp)

	// 创建请求
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("创建请求失败: %v", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TLS-Timestamp", timestamp)
	req.Header.Set("X-TLS-Signature", signature)
	req.Header.Set("X-TLS-AccessKey", l.accessKey)

	// 发送请求
	resp, err := l.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("发送请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 读取响应
	_, err = io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %v", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("请求失败，状态码: %d", resp.StatusCode)
	}

	return nil
}

// generateSignature 生成火山引擎签名
func (l *BytePlusLogger) generateSignature(method, url, body, timestamp string) string {
	// 提取 URL 中的 host 和 path
	parts := strings.SplitN(strings.TrimPrefix(url, "https://"), "/", 2)
	host := parts[0]
	path := "/" + parts[1]

	// 构建签名字符串
	signStr := fmt.Sprintf("%s\n%s\n%s\n%s\n%s", method, host, path, timestamp, body)

	// 使用 HMAC-SHA256 签名
	mac := hmac.New(sha256.New, []byte(l.secretKey))
	mac.Write([]byte(signStr))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return signature
}

// GetBytePlusLogger 从环境变量创建日志客户端
func GetBytePlusLogger() *BytePlusLogger {
	return NewBytePlusLogger(
		os.Getenv("BYTEPLUS_ENDPOINT"),
		os.Getenv("BYTEPLUS_ACCESS_KEY"),
		os.Getenv("BYTEPLUS_SECRET_KEY"),
		os.Getenv("BYTEPLUS_TOPIC_ID"),
	)
}

// mapToLogContents 将 map 转换为 LogContents
func mapToLogContents(fields map[string]string) []LogContentEntry {
	var contents []LogContentEntry
	// 按 key 排序，确保签名一致性
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		contents = append(contents, LogContentEntry{
			Key:   k,
			Value: fields[k],
		})
	}
	return contents
}
