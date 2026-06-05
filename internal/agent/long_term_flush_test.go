package agent

import (
	"testing"

	"stock_rag/internal/pkgctx"
)

func TestSummaryFingerprintChangesWithNewFact(t *testing.T) {
	a := &pkgctx.ConversationSummary{ConfirmedFacts: []string{"PE 25"}}
	b := &pkgctx.ConversationSummary{ConfirmedFacts: []string{"PE 25", "ROE 30%"}}
	if summaryFingerprint(a) == summaryFingerprint(b) {
		t.Fatal("expected different fingerprints")
	}
}

func TestSummaryFingerprintStable(t *testing.T) {
	s := &pkgctx.ConversationSummary{
		CurrentObject:  "600519",
		ConfirmedFacts: []string{"b", "a"},
	}
	if summaryFingerprint(s) != summaryFingerprint(s) {
		t.Fatal("expected stable fingerprint")
	}
}

func TestSummaryFingerprintEmpty(t *testing.T) {
	if summaryFingerprint(&pkgctx.ConversationSummary{}) != "" {
		t.Fatal("expected empty fingerprint")
	}
}
