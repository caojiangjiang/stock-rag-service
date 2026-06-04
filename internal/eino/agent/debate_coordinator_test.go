package agent

import (
	"context"
	"strings"
	"testing"
)

type stubDebateGenerator struct {
	calls int
}

func (s *stubDebateGenerator) GenerateArgument(
	ctx context.Context,
	profile *AgentProfile,
	taskState *TaskState,
	peerArguments []DebatePeerArgument,
	round int,
) (string, error) {
	s.calls++
	label := profile.Name
	if round > 1 && len(peerArguments) > 0 {
		label += "-rebuttal"
	}
	return label + ":" + taskState.UserMessage, nil
}

func TestDebateCoordinator_CustomArgumentGenerator(t *testing.T) {
	registry := NewProfileRegistry()
	builder := NewAgentBuilder(nil)
	factory := NewCoordinatorFactory(registry, builder)

	coord, err := factory.Create(CoordinatorTypeDebate)
	if err != nil {
		t.Fatalf("create debate coordinator: %v", err)
	}
	debate := coord.(*DebateCoordinator)

	profiles := []*AgentProfile{
		NewAgentProfile("p1", "角色一", "prompt1"),
		NewAgentProfile("p2", "角色二", "prompt2"),
	}
	debate.SetAgentProfiles(profiles)
	debate.SetMaxRounds(2)

	gen := &stubDebateGenerator{}
	debate.SetArgumentGenerator(gen)

	taskState := NewTaskState("c1", "m1", "u1", "测试议题")
	summary, err := debate.Execute(context.Background(), taskState)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gen.calls != 4 {
		t.Errorf("expected 4 generator calls (2 rounds x 2 agents), got %d", gen.calls)
	}
	if !strings.Contains(summary, "辩论结束") {
		t.Errorf("expected debate summary, got %q", summary)
	}
	if taskState.Status != TaskStatusCompleted {
		t.Errorf("expected completed status, got %s", taskState.Status)
	}
}
