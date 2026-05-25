package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"stock_rag/internal/pkgctx"
)

func TestCoordinatorRuntime_RunSubTask_Success(t *testing.T) {
	rt := NewCoordinatorRuntime("workflow", nil)
	taskState := NewTaskState("c1", "m1", "u1", "分析茅台")

	result, err := rt.RunSubTask(context.Background(), taskState, "evidence_collector", func(ctx context.Context) (string, error) {
		return "evidence ok", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "evidence ok" {
		t.Fatalf("result=%q", result)
	}
	if len(taskState.StepTraces) != 1 {
		t.Fatalf("step traces=%d", len(taskState.StepTraces))
	}
	if taskState.StepTraces[0].Status != TaskStatusCompleted {
		t.Fatalf("status=%s", taskState.StepTraces[0].Status)
	}
}

func TestCoordinatorRuntime_RunSubTask_RetryThenSuccess(t *testing.T) {
	strategy := pkgctx.DefaultExecutorStrategy()
	strategy.RetryPolicy.MaxRetries = 2
	strategy.RetryPolicy.InitialDelayMs = 1
	rt := NewCoordinatorRuntime("workflow", &strategy)
	taskState := NewTaskState("c1", "m1", "u1", "task")

	attempts := 0
	result, err := rt.RunSubTask(context.Background(), taskState, "metric_extractor", func(ctx context.Context) (string, error) {
		attempts++
		if attempts < 2 {
			return "", errors.New("temporary failure")
		}
		return "metrics ok", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "metrics ok" {
		t.Fatalf("result=%q", result)
	}
	if attempts != 2 {
		t.Fatalf("attempts=%d", attempts)
	}
}

func TestCoordinatorRuntime_DeriveContextTimeout(t *testing.T) {
	strategy := pkgctx.DefaultExecutorStrategy()
	strategy.SubAgentTimeoutMs = 50
	rt := NewCoordinatorRuntime("plan", &strategy)

	ctx, cancel := rt.DeriveContext(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected deadline")
	}
	if time.Until(deadline) > 100*time.Millisecond {
		t.Fatalf("deadline too far: %v", deadline)
	}
}
