package circuitbreaker

import (
	"errors"
	"testing"
	"time"
)

func TestBreakerOpensAfterFailures(t *testing.T) {
	b := New(Config{FailureThreshold: 2, OpenTimeout: time.Second, SuccessThreshold: 1})

	errFail := errors.New("fail")
	_ = b.Execute(func() error { return errFail })
	err := b.Execute(func() error { return errFail })
	if err == nil {
		t.Fatal("expected failure")
	}

	err = b.Execute(func() error { return nil })
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("expected circuit open, got %v", err)
	}
}

func TestBreakerRecoversAfterOpenTimeout(t *testing.T) {
	b := New(Config{FailureThreshold: 1, OpenTimeout: 20 * time.Millisecond, SuccessThreshold: 1})

	_ = b.Execute(func() error { return errors.New("fail") })
	time.Sleep(25 * time.Millisecond)

	err := b.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("expected recovery, got %v", err)
	}
	if b.State() != "closed" {
		t.Fatalf("expected closed state, got %s", b.State())
	}
}
