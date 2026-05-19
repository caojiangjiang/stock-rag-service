package api

import "testing"

func TestNewHealthResponseDefaultsToSkeleton(t *testing.T) {
	t.Setenv("ARK_API_KEY", "")
	t.Setenv("ARK_MODEL", "")

	resp := NewHealthResponse()
	if resp.Mode != "skeleton" {
		t.Fatalf("expected skeleton mode, got %s", resp.Mode)
	}
}

func TestNewHealthResponseUsesArkModeWhenConfigured(t *testing.T) {
	t.Setenv("ARK_API_KEY", "test-key")
	t.Setenv("ARK_MODEL", "ep-test")

	resp := NewHealthResponse()
	if resp.Mode != "ark" {
		t.Fatalf("expected ark mode, got %s", resp.Mode)
	}
}
