package agent

import (
	"context"
	"testing"

	"stock_rag/internal/repository"
	"stock_rag/internal/router"
)

func TestSaveAssistantMessageDoesNotOverwriteUserMessage(t *testing.T) {
	store := repository.NewMemoryConversationStore()
	ctx := context.Background()
	convID := "conversation-test-1"
	userID := "user-1"

	if err := store.SaveConversation(ctx, repository.NewConversation(convID, userID, "test")); err != nil {
		t.Fatalf("save conversation: %v", err)
	}

	userMsg := repository.NewMessage(convID, userID, "user", "用户提问内容", nil)

	svc := &ChatService{conversation: store}
	cc := &chatContext{
		ctx:           ctx,
		convID:        convID,
		req:           &ChatRequest{UserID: userID, Message: "用户提问内容"},
		userMsg:       userMsg,
		routeDecision: &router.RouteDecision{SelectedMode: router.ModeAgent},
		executeReq:    &ExecuteRequest{CoordinatorType: "supervisor"},
		executeResp: &ExecuteResponse{
			MessageID: userMsg.ID, // agent 路径曾错误复用 user message id
			Content:   "助手回答内容",
		},
	}

	if err := store.SaveMessage(ctx, userMsg); err != nil {
		t.Fatalf("save user: %v", err)
	}
	if err := svc.saveAssistantMessage(cc); err != nil {
		t.Fatalf("save assistant: %v", err)
	}

	msgs, err := store.GetMessages(ctx, convID, 10)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "用户提问内容" {
		t.Fatalf("user message lost or wrong: %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "助手回答内容" {
		t.Fatalf("assistant message wrong: %+v", msgs[1])
	}
	if msgs[0].ID == msgs[1].ID {
		t.Fatalf("assistant reused user message id: %s", msgs[0].ID)
	}
}
