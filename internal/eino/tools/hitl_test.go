package tools

import "testing"

func TestRequiresHITLApproval(t *testing.T) {
	if !RequiresHITLApproval("web_search") {
		t.Fatal("web_search should require approval")
	}
	if RequiresHITLApproval("extract_metrics") {
		t.Fatal("extract_metrics should not require approval")
	}
}

func TestParseToolApproval(t *testing.T) {
	if !parseToolApproval("确认继续") {
		t.Fatal("expected approved")
	}
	if parseToolApproval("拒绝") {
		t.Fatal("expected denied")
	}
	if !parseToolApproval(map[string]interface{}{"approved": true}) {
		t.Fatal("expected approved from map")
	}
}
