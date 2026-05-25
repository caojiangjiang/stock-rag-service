package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"stock_rag/internal/agent"
	"stock_rag/internal/metrics"
	"stock_rag/internal/observability"
	"stock_rag/internal/pkg/httpmiddleware"
)

func TestChatStreamSupportsFlusherThroughMiddleware(t *testing.T) {
	handler := NewChatHandler(&fakeChatService{resp: &agent.ChatResponse{
		Content: "hi",
		Mode:    "chat",
	}})
	streamHandler := httpmiddleware.Timeout(120 * time.Second)(handler.ChatStream)

	root := metrics.HTTPMetricsMiddleware(
		observability.TracingMiddleware("test")(
			http.HandlerFunc(streamHandler),
		),
	)

	body := bytes.NewBufferString(`{"message":"你好"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/chat/stream", body)
	rec := httptest.NewRecorder()

	root.ServeHTTP(rec, req)

	if rec.Code == http.StatusInternalServerError && rec.Body.String() == "{\"error\":\"streaming unsupported\"}\n" {
		t.Fatalf("middleware stripped http.Flusher: %s", rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("expected SSE response, got status=%d body=%q", rec.Code, rec.Body.String())
	}
}
