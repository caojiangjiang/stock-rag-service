package memory_test

import (
	"context"
	"testing"

	"stock_rag/internal/memory"
)

func TestFacadeNilTiers(t *testing.T) {
	m := memory.New(memory.DefaultConfig(), memory.Dependencies{})
	if m.Short() != nil || m.Medium() != nil || m.Long() != nil {
		t.Fatal("expected nil tiers when dependencies are empty")
	}
	if err := m.InitSchema(context.Background()); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := memory.DefaultConfig()
	if cfg.ShortMaxMsgs != 20 {
		t.Fatalf("ShortMaxMsgs = %d, want 20", cfg.ShortMaxMsgs)
	}
}
