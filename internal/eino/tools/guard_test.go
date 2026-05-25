package tools

import (
	"context"
	"errors"
	"testing"
	"time"

	"stock_rag/internal/pkg/circuitbreaker"
	"stock_rag/internal/pkg/retry"
)

func TestToolGuardRetriesThenSucceeds(t *testing.T) {
	guard := NewToolGuard(GuardConfig{
		DefaultTimeout: time.Second,
		Retry: retry.Config{
			MaxAttempts:  3,
			InitialDelay: time.Millisecond,
			MaxDelay:     time.Millisecond,
			Multiplier:   1,
		},
		Circuit: circuitbreaker.DefaultConfig,
	})

	attempts := 0
	out, err := guard.Run(context.Background(), "retrieve_evidence", func(ctx context.Context) (string, error) {
		attempts++
		if attempts < 2 {
			return "", errors.New("timeout")
		}
		return `{"ok":true}`, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != `{"ok":true}` {
		t.Fatalf("unexpected output: %s", out)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestToolGuardCircuitOpenReturnsDegraded(t *testing.T) {
	guard := NewToolGuard(GuardConfig{
		DefaultTimeout: time.Second,
		Retry: retry.Config{
			MaxAttempts:  1,
			InitialDelay: time.Millisecond,
			MaxDelay:     time.Millisecond,
			Multiplier:   1,
		},
		Circuit: circuitbreaker.Config{
			FailureThreshold: 1,
			OpenTimeout:      time.Minute,
			SuccessThreshold: 1,
		},
	})

	fail := func(context.Context) (string, error) {
		return "", errors.New("upstream error")
	}

	_, _ = guard.Run(context.Background(), "web_search", fail)
	out, err := guard.Run(context.Background(), "web_search", fail)
	if err != nil {
		t.Fatalf("circuit open should not return error to caller, got %v", err)
	}
	if !IsDegradedResponse(out) {
		t.Fatalf("expected degraded response, got %s", out)
	}
}

func TestToolRegistryInvokeUsesGuard(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&stubTool{name: "calculator"}, "metric_extractor")

	out, err := reg.Invoke(context.Background(), "calculator", map[string]interface{}{"query": "1+1"})
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	if out != "ok" {
		t.Fatalf("unexpected output: %s", out)
	}
}

type stubTool struct {
	name string
}

func (s *stubTool) Name() string        { return s.name }
func (s *stubTool) Description() string { return "stub" }
func (s *stubTool) Schema() string      { return `{}` }
func (s *stubTool) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	return "ok", nil
}
