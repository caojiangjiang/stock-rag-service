package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"stock_rag/internal/agent"
	"stock_rag/internal/concurrency"
)

type fakeChatService struct {
	resp   *agent.ChatResponse
	err    error
	called bool
}

func (f *fakeChatService) Chat(ctx context.Context, req *agent.ChatRequest) (*agent.ChatResponse, error) {
	f.called = true
	return f.resp, f.err
}

func (f *fakeChatService) ChatStream(ctx context.Context, req *agent.ChatRequest, onChunk func(string) error) (*agent.ChatResponse, error) {
	f.called = true
	if onChunk != nil && f.resp != nil && f.resp.Content != "" {
		_ = onChunk(f.resp.Content)
	}
	return f.resp, f.err
}

func TestChatHandlerRejectsUnknownFields(t *testing.T) {
	handler := NewChatHandler(&fakeChatService{})
	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewBufferString(`{"message":"你好","unexpected":true}`))
	resp := httptest.NewRecorder()

	handler.Chat(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.Code)
	}
}

func TestChatHandlerMapsQueueFullToTooManyRequests(t *testing.T) {
	handler := NewChatHandler(&fakeChatService{err: concurrency.ErrQueueFull})
	body := bytes.NewBufferString(`{"message":"你好"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/chat", body)
	resp := httptest.NewRecorder()

	handler.Chat(resp, req)

	if resp.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d", http.StatusTooManyRequests, resp.Code)
	}
}

func TestChatHandlerReturnsChatResponse(t *testing.T) {
	service := &fakeChatService{resp: &agent.ChatResponse{
		ConversationID: "conversation-1",
		MessageID:      "msg-2",
		Content:        "你好，我可以帮助你分析股票。",
		Mode:           "chat",
	}}
	handler := NewChatHandler(service)
	body := bytes.NewBufferString(`{"message":"你好","mode":"chat"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/chat", body)
	resp := httptest.NewRecorder()

	handler.Chat(resp, req)

	if !service.called {
		t.Fatal("expected chat service to be called")
	}
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.Code)
	}

	var got agent.ChatResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got.ConversationID != "conversation-1" {
		t.Fatalf("expected conversation_id to be returned, got %+v", got)
	}
	if got.MessageID != "msg-2" {
		t.Fatalf("expected message_id msg-2, got %s", got.MessageID)
	}
}