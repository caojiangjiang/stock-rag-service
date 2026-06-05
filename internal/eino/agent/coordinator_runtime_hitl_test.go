package agent

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
)

func TestProcessADKIterator_Interrupt(t *testing.T) {
	rt := NewCoordinatorRuntime("test", nil)
	taskState := NewTaskState("conv-1", "msg-1", "user-1", "测试")

	iter, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	generator.Send(&adk.AgentEvent{
		AgentName: "test-agent",
		Action: &adk.AgentAction{
			Interrupted: &adk.InterruptInfo{
				InterruptContexts: []*adk.InterruptCtx{{
					ID:   "interrupt-1",
					Info: "请确认是否继续",
				}},
			},
		},
	})
	generator.Close()

	result, err := rt.ProcessADKIterator(context.Background(), taskState, iter)
	if err != nil {
		t.Fatalf("ProcessADKIterator: %v", err)
	}
	if result == nil || result.Interrupt == nil {
		t.Fatal("expected interrupt result")
	}
	if result.Interrupt.ID != "interrupt-1" {
		t.Fatalf("unexpected interrupt id: %s", result.Interrupt.ID)
	}
	if taskState.Status != TaskStatusAwaitingHuman {
		t.Fatalf("expected awaiting_human, got %s", taskState.Status)
	}
}
