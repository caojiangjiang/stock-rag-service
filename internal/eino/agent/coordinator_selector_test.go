package agent

import (
	"context"
	"testing"

	"stock_rag/internal/router"
)

func TestCoordinatorSelector_Explicit(t *testing.T) {
	sel := NewCoordinatorSelector(
		DefaultCoordinatorSelectConfig(),
		NewDefaultCoordinatorRuleMatcher(),
		nil,
		nil,
	)
	dec, err := sel.Select(context.Background(), &CoordinatorSelectInput{
		CurrentMessage:      "随便问",
		ExplicitCoordinator: CoordinatorTypePlan,
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.SelectedType != CoordinatorTypePlan || dec.ClassifierType != "explicit" {
		t.Fatalf("got %+v", dec)
	}
}

func TestCoordinatorSelector_RulePlan(t *testing.T) {
	sel := NewCoordinatorSelector(
		DefaultCoordinatorSelectConfig(),
		NewDefaultCoordinatorRuleMatcher(),
		nil,
		nil,
	)
	dec, err := sel.Select(context.Background(), &CoordinatorSelectInput{
		CurrentMessage: "请分步骤对比茅台和五粮液2023年和2024年营收",
	})
	if err != nil {
		t.Fatal(err)
	}
	// plan 规则与 supervisor_compare 可能同时命中，取更高 confidence
	if dec.ClassifierType != "rule" {
		t.Fatalf("expected rule, got %s: %+v", dec.ClassifierType, dec)
	}
	if dec.SelectedType != CoordinatorTypePlan {
		t.Fatalf("expected plan, got %s (%s)", dec.SelectedType, dec.Reason)
	}
}

func TestCoordinatorSelector_ComplexityFallback(t *testing.T) {
	cfg := DefaultCoordinatorSelectConfig()
	cfg.EnableLLMClassifier = false
	sel := NewCoordinatorSelector(cfg, nil, nil, nil)
	dec, err := sel.Select(context.Background(), &CoordinatorSelectInput{
		CurrentMessage: "你好",
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.ClassifierType != "complexity" && dec.ClassifierType != "default" {
		t.Fatalf("unexpected classifier %s", dec.ClassifierType)
	}
	if dec.SelectedType != CoordinatorTypeSupervisor {
		t.Fatalf("expected supervisor fallback, got %s", dec.SelectedType)
	}
}

func TestCoordinatorSelector_Stickiness(t *testing.T) {
	cfg := DefaultCoordinatorSelectConfig()
	cfg.EnableLLMClassifier = false
	sel := NewCoordinatorSelector(cfg, nil, nil, nil)
	dec, err := sel.Select(context.Background(), &CoordinatorSelectInput{
		CurrentMessage:  "那2024年呢？",
		LastCoordinator: CoordinatorTypePlan,
		RecentMessages: []router.MessageContext{
			{Role: "user", Content: "分析茅台财报", RouteMode: router.ModeAgent},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.UserFollowUp || dec.SelectedType != CoordinatorTypePlan {
		t.Fatalf("expected plan stickiness, got %+v", dec)
	}
}

func TestCoordinatorSelector_LowConfidenceFallback(t *testing.T) {
	cfg := DefaultCoordinatorSelectConfig()
	sel := NewCoordinatorSelector(cfg, nil, &stubCoordinatorLLM{
		result: &CoordinatorLLMResult{
			Type:       CoordinatorTypeDebate,
			Confidence: 0.4,
			Reason:     "不确定",
		},
	}, nil)
	dec, err := sel.Select(context.Background(), &CoordinatorSelectInput{
		CurrentMessage: "分析一下",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.TriggeredFallback || dec.SelectedType != CoordinatorTypeSupervisor {
		t.Fatalf("expected supervisor fallback, got %+v", dec)
	}
}

type stubCoordinatorLLM struct {
	result *CoordinatorLLMResult
}

func (s *stubCoordinatorLLM) Classify(_ context.Context, _ *CoordinatorSelectInput, _ float64) (*CoordinatorLLMResult, error) {
	return s.result, nil
}
