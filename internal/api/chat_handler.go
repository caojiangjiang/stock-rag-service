package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"stock_rag/internal/agent"
	"stock_rag/internal/concurrency"
	"stock_rag/internal/observability"
	"stock_rag/internal/pkgctx"
	"stock_rag/internal/router"
)

type chatService interface {
	Chat(ctx context.Context, req *agent.ChatRequest) (*agent.ChatResponse, error)
}

type ChatHandler struct {
	agentService chatService
}

func NewChatHandler(agentService chatService) *ChatHandler {
	return &ChatHandler{agentService: agentService}
}

func (h *ChatHandler) Chat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		observability.L().WarnCtx(r.Context(), "Invalid HTTP method", "method", r.Method, "handler", "ChatHandler")
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		return
	}

	if h.agentService == nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "chat service unavailable"})
		return
	}

	defer r.Body.Close()

	var req agent.ChatRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		observability.L().ErrorCtx(r.Context(), "Failed to decode request body", err, "handler", "ChatHandler")
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "message is required"})
		return
	}

	if req.Mode != "" && !isValidChatMode(req.Mode) {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid mode"})
		return
	}

	observability.L().InfoCtx(r.Context(), "Received chat request",
		"conversation_id", req.ConversationID,
		"user_id", req.UserID,
		"message_preview", truncateForLog(req.Message, 120),
		"mode", req.Mode,
		"handler", "ChatHandler",
	)

	ctx := r.Context()

	// 生成并设置 traceId
	traceID := pkgctx.GenerateTraceID()
	ctx = pkgctx.WithTraceID(ctx, traceID)

	resp, err := h.agentService.Chat(ctx, &req)
	if err != nil || (resp != nil && resp.Error != "") {
		status := chatErrorStatus(err)
		message := "chat failed"
		if resp != nil && resp.Error != "" {
			message = resp.Error
		} else if err != nil {
			message = err.Error()
		}

		observability.L().ErrorCtx(ctx, "Chat service error", nil,
			"error", message,
			"conversation_id", req.ConversationID,
			"handler", "ChatHandler",
		)
		writeJSON(w, status, ErrorResponse{Error: message})
		return
	}

	if resp == nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "empty chat response"})
		return
	}

	observability.L().InfoCtx(ctx, "Chat response completed",
		"conversation_id", resp.ConversationID,
		"message_id", resp.MessageID,
		"mode", resp.Mode,
		"latency_ms", resp.LatencyMs,
		"handler", "ChatHandler",
	)

	writeJSON(w, http.StatusOK, resp)
}

func isValidChatMode(mode string) bool {
	switch router.RouteMode(mode) {
	case router.ModeChat, router.ModeRAG, router.ModeAnalysis, router.ModeAgent:
		return true
	default:
		return false
	}
}

func chatErrorStatus(err error) int {
	switch {
	case errors.Is(err, concurrency.ErrQueueFull):
		return http.StatusTooManyRequests
	case errors.Is(err, concurrency.ErrWaitTimeout), errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	case errors.Is(err, context.Canceled):
		return http.StatusRequestTimeout
	default:
		return http.StatusInternalServerError
	}
}

func truncateForLog(message string, maxLen int) string {
	message = strings.TrimSpace(message)
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(message)
	if len(runes) <= maxLen {
		return message
	}
	return string(runes[:maxLen]) + "..."
}
