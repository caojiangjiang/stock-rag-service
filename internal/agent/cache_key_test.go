package agent

import "testing"

func TestBuildExactCacheKeyStable(t *testing.T) {
	key1 := buildExactCacheKey("question", "rag", "600519", "report", "latest")
	key2 := buildExactCacheKey("question", "rag", "600519", "report", "latest")
	if key1 != key2 {
		t.Fatalf("expected stable cache key, got %q vs %q", key1, key2)
	}

	key3 := buildExactCacheKey("question", "chat", "600519", "report", "latest")
	if key1 == key3 {
		t.Fatal("expected different cache keys for different modes")
	}
}
