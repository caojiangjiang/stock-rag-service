package adapter

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestNormalizeModelMessage_DoubaoTransferToAgent(t *testing.T) {
	raw := `<|FunctionCallBegin|>[{"name":"transfer_to_agent","parameters":{"agent_name":"analyst_writer"}}]<|FunctionCallEnd|>`
	msg := &schema.Message{Role: schema.Assistant, Content: raw}

	out := normalizeModelMessage(msg)
	if len(out.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(out.ToolCalls))
	}
	if out.ToolCalls[0].Function.Name != "transfer_to_agent" {
		t.Fatalf("unexpected tool name: %s", out.ToolCalls[0].Function.Name)
	}
	if out.ToolCalls[0].Function.Arguments != `{"agent_name":"analyst_writer"}` {
		t.Fatalf("unexpected args: %s", out.ToolCalls[0].Function.Arguments)
	}
	if out.Content != "" {
		t.Fatalf("expected empty content after normalize, got %q", out.Content)
	}
}
