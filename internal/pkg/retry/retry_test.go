package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDoSucceedsWithoutRetry(t *testing.T) {
	attempts := 0
	err := Do(context.Background(), DefaultConfig, func() error {
		attempts++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
}

func TestDoRetriesTemporaryError(t *testing.T) {
	cfg := Config{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     time.Millisecond,
		Multiplier:   1,
	}
	attempts := 0
	err := Do(context.Background(), cfg, func() error {
		attempts++
		if attempts < 3 {
			return &TemporaryError{Err: errors.New("timeout")}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestDoStopsOnPermanentError(t *testing.T) {
	attempts := 0
	err := Do(context.Background(), DefaultConfig, func() error {
		attempts++
		return &PermanentError{Err: errors.New("bad request")}
	})
	if err == nil {
		t.Fatal("expected permanent error")
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
}

func TestIsTemporary(t *testing.T) {
	if !IsTemporary(&TemporaryError{Err: errors.New("x")}) {
		t.Fatal("TemporaryError should be temporary")
	}
	if IsTemporary(&PermanentError{Err: errors.New("x")}) {
		t.Fatal("PermanentError should not be temporary")
	}
	if !IsTemporary(errors.New("connection reset by peer")) {
		t.Fatal("network errors should be temporary")
	}
}
