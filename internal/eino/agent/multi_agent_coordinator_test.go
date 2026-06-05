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

func TestMultiAgentCoordinator_DebateTopology(t *testing.T) {
	registry := NewProfileRegistry()
	builder := NewAgentBuilder(nil)
	factory := NewCoordinatorFactory(registry, builder, nil, nil)

	coord, err := factory.Create(CoordinatorTypeDebate)
	if err != nil {
		t.Fatalf("create debate coordinator: %v", err)
	}
	multi, ok := coord.(*MultiAgentCoordinator)
	if !ok {
		t.Fatalf("expected MultiAgentCoordinator, got %T", coord)
	}
	if multi.Topology() != TopologyDebate {
		t.Fatalf("expected debate topology, got %s", multi.Topology())
	}

	profiles := []*AgentProfile{
		NewAgentProfile("p1", "角色一", "prompt1"),
		NewAgentProfile("p2", "角色二", "prompt2"),
	}
	multi.SetAgentProfiles(profiles)
	multi.SetMaxRounds(2)

	gen := &stubDebateGenerator{}
	multi.SetArgumentGenerator(gen)

	taskState := NewTaskState("c1", "m1", "u1", "测试议题")
	summary, err := multi.Execute(context.Background(), taskState)
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

func TestNormalizeCoordinatorType(t *testing.T) {
	cases := []struct {
		input    CoordinatorType
		expected CoordinatorType
		topology MultiAgentTopology
		fixed    bool
	}{
		{CoordinatorTypeDebate, CoordinatorTypeMultiAgent, TopologyDebate, false},
		{CoordinatorTypePeer, CoordinatorTypeMultiAgent, TopologyParallel, false},
		{CoordinatorTypeCommittee, CoordinatorTypeMultiAgent, TopologyCommittee, false},
		{CoordinatorTypePipeline, CoordinatorTypePlan, "", true},
		{CoordinatorTypeSupervisor, CoordinatorTypeSupervisor, "", false},
	}

	for _, tc := range cases {
		normalized, opts := NormalizeCoordinatorType(tc.input)
		if normalized != tc.expected {
			t.Errorf("%s: expected type %s, got %s", tc.input, tc.expected, normalized)
		}
		if opts.MultiAgentTopology != tc.topology {
			t.Errorf("%s: expected topology %s, got %s", tc.input, tc.topology, opts.MultiAgentTopology)
		}
		if opts.PlanFixedSteps != tc.fixed {
			t.Errorf("%s: expected fixedSteps %v, got %v", tc.input, tc.fixed, opts.PlanFixedSteps)
		}
	}
}

func TestCoordinatorFactory_PipelineUsesFixedSteps(t *testing.T) {
	factory := NewCoordinatorFactory(NewProfileRegistry(), NewAgentBuilder(nil), nil, nil)
	coord, err := factory.Create(CoordinatorTypePipeline)
	if err != nil {
		t.Fatal(err)
	}
	plan, ok := coord.(*PlanCoordinator)
	if !ok {
		t.Fatalf("expected PlanCoordinator, got %T", coord)
	}
	if !plan.FixedSteps() {
		t.Fatal("pipeline alias should create plan with fixed steps")
	}
}
