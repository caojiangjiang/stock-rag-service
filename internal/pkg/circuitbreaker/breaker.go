package circuitbreaker

import (
	"errors"
	"sync"
	"time"
)

// ErrOpen 熔断器打开时返回。
var ErrOpen = errors.New("circuit breaker is open")

// Config 熔断配置。
type Config struct {
	FailureThreshold int           // 连续失败次数达到后打开熔断
	OpenTimeout      time.Duration // 打开后多久进入半开
	SuccessThreshold int           // 半开状态下连续成功次数后关闭
}

// DefaultConfig 默认熔断配置。
var DefaultConfig = Config{
	FailureThreshold: 5,
	OpenTimeout:      30 * time.Second,
	SuccessThreshold: 2,
}

type state int

const (
	stateClosed state = iota
	stateOpen
	stateHalfOpen
)

// Breaker 按名称隔离的简单熔断器。
type Breaker struct {
	cfg Config

	mu            sync.Mutex
	current       state
	failures      int
	halfOpenWins  int
	openedAt      time.Time
}

// New 创建熔断器。
func New(cfg Config) *Breaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = DefaultConfig.FailureThreshold
	}
	if cfg.OpenTimeout <= 0 {
		cfg.OpenTimeout = DefaultConfig.OpenTimeout
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = DefaultConfig.SuccessThreshold
	}
	return &Breaker{cfg: cfg, current: stateClosed}
}

// Execute 在熔断保护下执行 fn。
func (b *Breaker) Execute(fn func() error) error {
	if err := b.beforeCall(); err != nil {
		return err
	}

	err := fn()
	b.afterCall(err)
	return err
}

func (b *Breaker) beforeCall() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.current {
	case stateOpen:
		if time.Since(b.openedAt) >= b.cfg.OpenTimeout {
			b.current = stateHalfOpen
			b.halfOpenWins = 0
			return nil
		}
		return ErrOpen
	case stateHalfOpen, stateClosed:
		return nil
	default:
		return nil
	}
}

func (b *Breaker) afterCall(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err == nil {
		b.onSuccess()
		return
	}
	b.onFailure()
}

func (b *Breaker) onSuccess() {
	switch b.current {
	case stateHalfOpen:
		b.halfOpenWins++
		if b.halfOpenWins >= b.cfg.SuccessThreshold {
			b.current = stateClosed
			b.failures = 0
			b.halfOpenWins = 0
		}
	case stateClosed:
		b.failures = 0
	}
}

func (b *Breaker) onFailure() {
	b.failures++
	switch b.current {
	case stateHalfOpen:
		b.current = stateOpen
		b.openedAt = time.Now()
		b.halfOpenWins = 0
	case stateClosed:
		if b.failures >= b.cfg.FailureThreshold {
			b.current = stateOpen
			b.openedAt = time.Now()
		}
	}
}

// State 返回当前状态字符串（用于监控）。
func (b *Breaker) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.current {
	case stateOpen:
		return "open"
	case stateHalfOpen:
		return "half_open"
	default:
		return "closed"
	}
}
