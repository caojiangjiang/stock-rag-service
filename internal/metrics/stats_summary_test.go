package metrics

import "testing"

func TestGatherSummaryEmpty(t *testing.T) {
	summary := GatherSummary()
	if summary.Cache.HitRate != 0 {
		t.Fatalf("expected zero hit rate, got %f", summary.Cache.HitRate)
	}
}

func TestRecordCacheUpdatesHitRatio(t *testing.T) {
	cacheHitsAtomic.Store(0)
	cacheMissesAtomic.Store(0)

	RecordCacheHit("semantic")
	RecordCacheMiss("semantic")

	summary := GatherSummary()
	if summary.Cache.Hits < 1 || summary.Cache.Misses < 1 {
		t.Fatalf("expected hits and misses, got %+v", summary.Cache)
	}
	if summary.Cache.HitRate <= 0 || summary.Cache.HitRate >= 1 {
		t.Fatalf("expected ratio between 0 and 1, got %f", summary.Cache.HitRate)
	}
}
