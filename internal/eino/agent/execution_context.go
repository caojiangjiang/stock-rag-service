package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"stock_rag/internal/pkgctx"
)

type ExecutionContext struct {
	RequestID     string
	Strategy      *pkgctx.ExecutorStrategyConfig
	StepCount     int
	ToolCallCount int
	TokenUsage    int
	StartTime     time.Time
	Errors        []string
	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
}

func NewExecutionContext(requestID string, strategy *pkgctx.ExecutorStrategyConfig) *ExecutionContext {
	var execStrategy pkgctx.ExecutorStrategyConfig
	if strategy == nil {
		execStrategy = pkgctx.DefaultExecutorStrategy()
	} else {
		execStrategy = *strategy
	}

	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(execStrategy.SubAgentTimeoutMs)*time.Millisecond)

	return &ExecutionContext{
		RequestID:     requestID,
		Strategy:      &execStrategy,
		StepCount:     0,
		ToolCallCount: 0,
		TokenUsage:    0,
		StartTime:     time.Now(),
		Errors:        make([]string, 0),
		ctx:           ctx,
		cancel:        cancel,
	}
}

func (e *ExecutionContext) Context() context.Context {
	return e.ctx
}

func (e *ExecutionContext) Cancel() {
	if e.cancel != nil {
		e.cancel()
	}
}

func (e *ExecutionContext) IncrementStep() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.StepCount++
	if e.StepCount > e.Strategy.MaxSteps {
		return fmt.Errorf("超过最大步骤数限制: %d", e.Strategy.MaxSteps)
	}
	return nil
}

func (e *ExecutionContext) IncrementToolCall() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.ToolCallCount++
	if e.ToolCallCount > e.Strategy.MaxToolCalls {
		return fmt.Errorf("超过最大工具调用次数限制: %d", e.Strategy.MaxToolCalls)
	}
	return nil
}

func (e *ExecutionContext) AddTokenUsage(tokens int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.TokenUsage += tokens
	if e.TokenUsage > e.Strategy.MaxTokenBudget {
		return fmt.Errorf("超过最大 token 预算: %d / %d", e.TokenUsage, e.Strategy.MaxTokenBudget)
	}
	return nil
}

func (e *ExecutionContext) AddError(err string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Errors = append(e.Errors, err)
}

func (e *ExecutionContext) GetErrors() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.Errors
}

func (e *ExecutionContext) CanContinue() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.StepCount >= e.Strategy.MaxSteps {
		return false
	}
	if e.ToolCallCount >= e.Strategy.MaxToolCalls {
		return false
	}
	if e.TokenUsage >= e.Strategy.MaxTokenBudget {
		return false
	}
	return true
}

func (e *ExecutionContext) ShouldFallback() bool {
	return e.Strategy.EnableFallback && !e.CanContinue()
}

func (e *ExecutionContext) GetFallbackStrategy() pkgctx.FallbackStrategy {
	return e.Strategy.FallbackStrategy
}

func (e *ExecutionContext) GetStatus() ExecutionStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()

	status := ExecutionStatus{
		RequestID:      e.RequestID,
		StepCount:      e.StepCount,
		ToolCallCount:  e.ToolCallCount,
		TokenUsage:     e.TokenUsage,
		MaxSteps:       e.Strategy.MaxSteps,
		MaxToolCalls:   e.Strategy.MaxToolCalls,
		MaxTokenBudget: e.Strategy.MaxTokenBudget,
		ElapsedMs:      time.Since(e.StartTime).Milliseconds(),
		Errors:         e.Errors,
	}

	if !e.CanContinue() {
		if e.StepCount >= e.Strategy.MaxSteps {
			status.Reason = "max_steps_exceeded"
		} else if e.ToolCallCount >= e.Strategy.MaxToolCalls {
			status.Reason = "max_tool_calls_exceeded"
		} else if e.TokenUsage >= e.Strategy.MaxTokenBudget {
			status.Reason = "max_token_budget_exceeded"
		}
	}

	return status
}

type ExecutionStatus struct {
	RequestID      string   `json:"request_id"`
	StepCount      int      `json:"step_count"`
	ToolCallCount  int      `json:"tool_call_count"`
	TokenUsage     int      `json:"token_usage"`
	MaxSteps       int      `json:"max_steps"`
	MaxToolCalls   int      `json:"max_tool_calls"`
	MaxTokenBudget int      `json:"max_token_budget"`
	ElapsedMs      int64    `json:"elapsed_ms"`
	Reason         string   `json:"reason,omitempty"`
	Errors         []string `json:"errors,omitempty"`
}

type RetryHandler struct {
	maxRetries        int
	initialDelayMs    int
	maxDelayMs        int
	backoffMultiplier float64
	currentRetry      int
}

func NewRetryHandler(strategy *pkgctx.ExecutorStrategyConfig) *RetryHandler {
	return &RetryHandler{
		maxRetries:        strategy.RetryPolicy.MaxRetries,
		initialDelayMs:    strategy.RetryPolicy.InitialDelayMs,
		maxDelayMs:        strategy.RetryPolicy.MaxDelayMs,
		backoffMultiplier: strategy.RetryPolicy.BackoffMultiplier,
		currentRetry:      0,
	}
}

func (r *RetryHandler) ShouldRetry() bool {
	return r.currentRetry < r.maxRetries
}

func (r *RetryHandler) GetDelayMs() int {
	delay := float64(r.initialDelayMs)
	for i := 0; i < r.currentRetry; i++ {
		delay *= r.backoffMultiplier
	}
	if delay > float64(r.maxDelayMs) {
		delay = float64(r.maxDelayMs)
	}
	return int(delay)
}

func (r *RetryHandler) RecordRetry() {
	r.currentRetry++
}

func (r *RetryHandler) Reset() {
	r.currentRetry = 0
}
