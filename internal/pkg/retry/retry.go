package retry

import (
	"context"
	"fmt"
	"math"
	"time"
)

// Config 重试配置
type Config struct {
	MaxAttempts  int           // 最大重试次数
	InitialDelay time.Duration // 初始延迟
	MaxDelay     time.Duration // 最大延迟
	Multiplier   float64       // 延迟倍数
}

// DefaultConfig 默认重试配置
var DefaultConfig = Config{
	MaxAttempts:  3,
	InitialDelay: 100 * time.Millisecond,
	MaxDelay:     5 * time.Second,
	Multiplier:   2.0,
}

// ErrorHandler 错误处理函数，返回 true 表示应该重试
type ErrorHandler func(err error) bool

// PermanentError 永久错误，不应重试
type PermanentError struct {
	Err error
}

func (e *PermanentError) Error() string {
	return e.Err.Error()
}

func (e *PermanentError) Unwrap() error {
	return e.Err
}

// TemporaryError 临时错误，应该重试
type TemporaryError struct {
	Err error
}

func (e *TemporaryError) Error() string {
	return e.Err.Error()
}

func (e *TemporaryError) Unwrap() error {
	return e.Err
}

// IsTemporary 判断错误是否是临时错误
func IsTemporary(err error) bool {
	if err == nil {
		return false
	}
	// 检查是否是 TemporaryError 类型
	if _, ok := err.(*TemporaryError); ok {
		return true
	}
	return isTemporaryError(err)
}

func isTemporaryError(err error) bool {
	// 检查常见临时错误
	if err == nil {
		return false
	}
	errStr := err.Error()
	// 网络超时、连接失败等通常是临时的
	temporaryIndicators := []string{
		"timeout",
		"connection refused",
		"connection reset",
		"network unreachable",
		"i/o timeout",
		"context deadline exceeded",
		"no such host",
		"temporary failure",
		"service unavailable",
		"too many requests",
		"rate limit",
	}
	for _, indicator := range temporaryIndicators {
		if containsIgnoreCase(errStr, indicator) {
			return true
		}
	}
	return false
}

func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if equalIgnoreCase(s[i:i+len(substr)], substr) {
			return true
		}
	}
	return false
}

func equalIgnoreCase(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// Do 执行带重试的操作
func Do(ctx context.Context, cfg Config, fn func() error) error {
	return DoWithErrorHandler(ctx, cfg, fn, IsTemporary)
}

// DoWithErrorHandler 执行带重试的操作，使用自定义错误处理器
func DoWithErrorHandler(ctx context.Context, cfg Config, fn func() error, shouldRetry ErrorHandler) error {
	var lastErr error
	delay := cfg.InitialDelay

	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			// 计算下一次延迟，使用 math.Min 确保不超过 MaxDelay
			delay = time.Duration(math.Min(float64(delay)*cfg.Multiplier, float64(cfg.MaxDelay)))
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		// 检查是否是永久错误
		if _, ok := lastErr.(*PermanentError); ok {
			return lastErr
		}

		// 检查是否应该重试
		if !shouldRetry(lastErr) {
			return lastErr
		}
	}

	return fmt.Errorf("重试次数耗尽 (attempts=%d): %w", cfg.MaxAttempts, lastErr)
}

// WithRetry 包装函数，使其支持重试
func WithRetry[T any](cfg Config, fn func() (T, error)) func() (T, error) {
	return func() (T, error) {
		var result T
		err := Do(context.Background(), cfg, func() error {
			var err error
			result, err = fn()
			return err
		})
		return result, err
	}
}

// RetryOnTemporary 只在临时错误时重试
func RetryOnTemporary(ctx context.Context, cfg Config, fn func() error) error {
	return Do(ctx, cfg, fn)
}
