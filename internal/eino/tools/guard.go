package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"stock_rag/internal/metrics"
	"stock_rag/internal/observability"
	"stock_rag/internal/pkg/circuitbreaker"
	"stock_rag/internal/pkg/retry"
)

// GuardConfig 工具调用防护配置。
type GuardConfig struct {
	DefaultTimeout time.Duration
	Retry          retry.Config
	Circuit        circuitbreaker.Config
	// Timeouts 按工具名覆盖默认超时。
	Timeouts map[string]time.Duration
}

// DefaultGuardConfig 默认防护配置。
func DefaultGuardConfig() GuardConfig {
	return GuardConfig{
		DefaultTimeout: 30 * time.Second,
		Retry:          retry.DefaultConfig,
		Circuit:        circuitbreaker.DefaultConfig,
		Timeouts: map[string]time.Duration{
			"retrieve_evidence": 60 * time.Second,
			"web_search":        45 * time.Second,
			"fetch_webpage":     45 * time.Second,
			"search_announcements": 45 * time.Second,
			"get_market_snapshot":  20 * time.Second,
			"calculator":           10 * time.Second,
			"normalize_units":      10 * time.Second,
			"dedupe_sources":       15 * time.Second,
		},
	}
}

// ErrCircuitOpen 工具熔断打开。
var ErrCircuitOpen = errors.New("tool circuit breaker open")

// ToolGuard 为工具调用提供超时、重试与熔断。
type ToolGuard struct {
	cfg      GuardConfig
	breakers map[string]*circuitbreaker.Breaker
	mu       sync.Mutex
}

// NewToolGuard 创建工具防护器。
func NewToolGuard(cfg GuardConfig) *ToolGuard {
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = DefaultGuardConfig().DefaultTimeout
	}
	if cfg.Retry.MaxAttempts <= 0 {
		cfg.Retry = DefaultGuardConfig().Retry
	}
	if cfg.Timeouts == nil {
		cfg.Timeouts = DefaultGuardConfig().Timeouts
	}
	return &ToolGuard{
		cfg:      cfg,
		breakers: make(map[string]*circuitbreaker.Breaker),
	}
}

// Run 执行工具调用（超时 + 重试 + 熔断）。
func (g *ToolGuard) Run(ctx context.Context, toolName string, fn func(context.Context) (string, error)) (string, error) {
	if g == nil {
		return fn(ctx)
	}

	start := time.Now()
	status := "success"
	defer func() {
		metrics.RecordToolCall(toolName, status, time.Since(start).Seconds())
	}()

	breaker := g.breakerFor(toolName)
	timeout := g.timeoutFor(toolName)

	retryCfg := g.cfg.Retry
	if nonRetryableTools()[toolName] {
		retryCfg.MaxAttempts = 1
	}

	var result string
	var lastCallErr error

	runErr := breaker.Execute(func() error {
		return retry.DoWithErrorHandler(ctx, retryCfg, func() error {
			callCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			var attemptErr error
			result, attemptErr = fn(callCtx)
			lastCallErr = attemptErr
			if attemptErr == nil {
				return nil
			}
			if errors.Is(attemptErr, context.DeadlineExceeded) || errors.Is(attemptErr, context.Canceled) {
				return &retry.TemporaryError{Err: attemptErr}
			}
			if !shouldRetryTool(toolName, attemptErr) {
				return &retry.PermanentError{Err: attemptErr}
			}
			if retry.IsTemporary(attemptErr) {
				return &retry.TemporaryError{Err: attemptErr}
			}
			return &retry.PermanentError{Err: attemptErr}
		}, retry.IsTemporary)
	})

	if runErr != nil {
		if errors.Is(runErr, circuitbreaker.ErrOpen) {
			status = "circuit_open"
			observability.L().WarnCtx(ctx, "Tool circuit open", "tool", toolName)
			// 降级 JSON 返回给 Agent，不中断整条链路
			return degradedToolResponse(toolName, ErrCircuitOpen), nil
		}
		status = "error"
		err := runErr
		if lastCallErr != nil {
			err = lastCallErr
		}
		return degradedToolResponse(toolName, err), err
	}

	return result, nil
}

func (g *ToolGuard) breakerFor(name string) *circuitbreaker.Breaker {
	g.mu.Lock()
	defer g.mu.Unlock()
	if b, ok := g.breakers[name]; ok {
		return b
	}
	b := circuitbreaker.New(g.cfg.Circuit)
	g.breakers[name] = b
	return b
}

func (g *ToolGuard) timeoutFor(name string) time.Duration {
	if d, ok := g.cfg.Timeouts[name]; ok && d > 0 {
		return d
	}
	return g.cfg.DefaultTimeout
}

func degradedToolResponse(toolName string, err error) string {
	payload := map[string]interface{}{
		"error":   "tool_degraded",
		"tool":    toolName,
		"message": err.Error(),
	}
	if errors.Is(err, ErrCircuitOpen) || errors.Is(err, circuitbreaker.ErrOpen) {
		payload["reason"] = "circuit_open"
		payload["message"] = "工具暂时不可用，已熔断保护，请稍后重试或换用其他工具"
	} else if errors.Is(err, context.DeadlineExceeded) {
		payload["reason"] = "timeout"
		payload["message"] = "工具调用超时"
	}
	data, _ := json.Marshal(payload)
	return string(data)
}

// nonRetryableTools 返回不应重试的工具名（纯本地计算）。
func nonRetryableTools() map[string]bool {
	return map[string]bool{
		"calculator":      true,
		"normalize_units": true,
		"dedupe_sources":  true,
		"resolve_entity":  true,
	}
}

func shouldRetryTool(toolName string, err error) bool {
	if nonRetryableTools()[toolName] {
		return false
	}
	return retry.IsTemporary(err)
}

// CircuitState 返回工具熔断状态。
func (g *ToolGuard) CircuitState(toolName string) string {
	if g == nil {
		return "disabled"
	}
	return g.breakerFor(toolName).State()
}

// FormatToolName 规范化工具名。
func FormatToolName(name string) string {
	return strings.TrimSpace(name)
}

// IsDegradedResponse 判断返回是否为降级 JSON。
func IsDegradedResponse(body string) bool {
	return strings.Contains(body, `"error":"tool_degraded"`) ||
		strings.Contains(body, `"error": "tool_degraded"`)
}

// DegradedMessage 从降级响应提取消息。
func DegradedMessage(body string) string {
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err == nil && payload.Message != "" {
		return payload.Message
	}
	return fmt.Sprintf("tool degraded: %s", truncate(body, 120))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
