package agent

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"stock_rag/internal/observability"
)

// FanoutResult 单个任务的执行结果
type FanoutResult[T any] struct {
	Index    int
	Result   T
	Error    error
	TaskName string
}

// FanoutOptions Fanout 执行选项
type FanoutOptions struct {
	Timeout        time.Duration
	MaxConcurrency int
	EnableTracing  bool
	TraceName      string
}

// FanoutOption 选项函数
type FanoutOption func(*FanoutOptions)

// WithTimeout 设置超时时间
func WithTimeout(timeout time.Duration) FanoutOption {
	return func(o *FanoutOptions) {
		o.Timeout = timeout
	}
}

// WithMaxConcurrency 设置最大并发数
func WithMaxConcurrency(max int) FanoutOption {
	return func(o *FanoutOptions) {
		o.MaxConcurrency = max
	}
}

// WithTracing 启用追踪
func WithTracing(name string) FanoutOption {
	return func(o *FanoutOptions) {
		o.EnableTracing = true
		o.TraceName = name
	}
}

// Fanout 通用并发执行函数
func Fanout[T any, I any](ctx context.Context, items []I, task func(context.Context, int, I) (T, error), options ...FanoutOption) []FanoutResult[T] {
	opts := &FanoutOptions{
		Timeout:        60 * time.Second,
		MaxConcurrency: len(items),
		EnableTracing:  true,
		TraceName:      "fanout",
	}

	for _, opt := range options {
		opt(opts)
	}

	if opts.MaxConcurrency <= 0 || opts.MaxConcurrency > len(items) {
		opts.MaxConcurrency = len(items)
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	resultCh := make(chan FanoutResult[T], len(items))
	sem := make(chan struct{}, opts.MaxConcurrency)

	for i, item := range items {
		go func(idx int, it I) {
			sem <- struct{}{}
			defer func() { <-sem }()

			var span trace.Span
			spanCtx := ctx
			if opts.EnableTracing {
				spanCtx, span = observability.StartSpan(ctx, fmt.Sprintf("%s.item", opts.TraceName))
				defer span.End()
			}

			result, err := task(spanCtx, idx, it)

			if span != nil {
				if err != nil {
					span.SetAttributes(attribute.String("error", err.Error()))
				}
				span.SetAttributes(attribute.String("task_name", fmt.Sprintf("item_%d", idx)))
			}

			resultCh <- FanoutResult[T]{
				Index:    idx,
				Result:   result,
				Error:    err,
				TaskName: fmt.Sprintf("item_%d", idx),
			}
		}(i, item)
	}

	results := make([]FanoutResult[T], len(items))
	for i := 0; i < len(items); i++ {
		select {
		case result := <-resultCh:
			results[result.Index] = result
		case <-ctx.Done():
			return results[:i]
		}
	}

	return results
}
