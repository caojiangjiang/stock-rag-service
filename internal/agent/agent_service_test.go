package agent

import (
	"context"
	"testing"

	"stock_rag/internal/repository"
	"stock_rag/internal/router"
)

type noopRuleMatcher struct{}

func (noopRuleMatcher) Match(input *router.RouteInput) ([]router.RuleMatch, error) {
	return nil, nil
}

type stubExecutor struct {
	mode    router.RouteMode
	content string
	lastReq *ExecuteRequest
}

func (s *stubExecutor) Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error) {
	copyReq := *req
	s.lastReq = &copyReq
	return &ExecuteResponse{Content: s.content, Mode: s.mode}, nil
}

func (s *stubExecutor) Name() string           { return "stub" }
func (s *stubExecutor) Mode() router.RouteMode { return s.mode }

func TestAgentServiceChatCreatesConversationAndReturnsIDs(t *testing.T) {
	store := repository.NewMemoryConversationStore()
	routeEngine := router.NewRouteEngine(router.DefaultRouteConfig(), nil, noopRuleMatcher{}, nil)
	exec := &stubExecutor{mode: router.ModeChat, content: "你好，我可以帮助你。"}
	svc := NewChatService(routeEngine, NewAgentExecutor(exec, nil, nil, nil), store)

	resp, err := svc.Chat(context.Background(), &ChatRequest{UserID: "u1", Message: "你好", Mode: "chat"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.ConversationID == "" {
		t.Fatal("expected conversation_id to be generated")
	}
	if resp.MessageID == "" {
		t.Fatal("expected message_id to be returned")
	}

	messages, err := store.GetMessages(context.Background(), resp.ConversationID, 10)
	if err != nil {
		t.Fatalf("failed to get messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 persisted messages, got %d", len(messages))
	}
}

func TestAgentServiceChatUsesRecentRouteModeForFollowUp(t *testing.T) {
	store := repository.NewMemoryConversationStore()
	convID := "conversation-1"
	if err := store.SaveConversation(context.Background(), repository.NewConversation(convID, "u1", "title")); err != nil {
		t.Fatalf("failed to save conversation: %v", err)
	}
	firstUser := repository.NewMessage(convID, "user", "你好", nil)
	firstUser.RouteMode = string(router.ModeChat)
	if err := store.SaveMessage(context.Background(), firstUser); err != nil {
		t.Fatalf("failed to save first user message: %v", err)
	}
	firstAssistant := repository.NewMessage(convID, "assistant", "你好，有什么可以帮你？", nil)
	firstAssistant.RouteMode = string(router.ModeChat)
	if err := store.SaveMessage(context.Background(), firstAssistant); err != nil {
		t.Fatalf("failed to save first assistant message: %v", err)
	}

	routeEngine := router.NewRouteEngine(router.DefaultRouteConfig(), nil, noopRuleMatcher{}, nil)
	exec := &stubExecutor{mode: router.ModeChat, content: "继续为你说明。"}
	svc := NewChatService(routeEngine, NewAgentExecutor(exec, nil, nil, nil), store)

	resp, err := svc.Chat(context.Background(), &ChatRequest{ConversationID: convID, UserID: "u1", Message: "继续"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if exec.lastReq == nil || exec.lastReq.Mode != router.ModeChat {
		t.Fatalf("expected follow-up to inherit chat mode, got %+v", exec.lastReq)
	}
	if resp.Mode != string(router.ModeChat) {
		t.Fatalf("expected response mode chat, got %s", resp.Mode)
	}
}
