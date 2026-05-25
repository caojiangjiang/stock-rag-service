package metrics

import (
	"testing"

	dto "github.com/prometheus/client_model/go"
)

func TestQuantileFromHistogram(t *testing.T) {
	upper1 := 1.0
	upper5 := 5.0
	c1 := uint64(10)
	c2 := uint64(100)
	sc := uint64(100)
	ss := 250.0
	h := &dto.Histogram{
		SampleCount: &sc,
		SampleSum:   &ss,
		Bucket: []*dto.Bucket{
			{UpperBound: &upper1, CumulativeCount: &c1},
			{UpperBound: &upper5, CumulativeCount: &c2},
		},
	}
	p95 := quantileFromHistogram(h, 0.95)
	if p95 <= 0 {
		t.Fatalf("p95=%v", p95)
	}
}

func TestGatherSummaryIncludesChatByMode(t *testing.T) {
	RecordChatRequest("rag", "success", 0.5)
	RecordChatRequest("agent", "success", 1.2)
	RecordChatRequest("chat", "error", 0.3)

	summary := GatherSummary()
	if len(summary.Chat.ByMode) == 0 {
		t.Fatal("expected by_mode stats")
	}
	if summary.Chat.Total < 3 {
		t.Fatalf("chat total=%v", summary.Chat.Total)
	}
}
