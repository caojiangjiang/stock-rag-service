package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"stock_rag/internal/agent"
	"stock_rag/internal/observability"
	"stock_rag/internal/pkgctx"
)

func (h *ChatHandler) ChatStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		observability.L().WarnCtx(r.Context(), "Invalid HTTP method", "method", r.Method, "handler", "ChatHandler.ChatStream")
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		return
	}

	if h.agentService == nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "chat service unavailable"})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "streaming unsupported"})
		return
	}

	defer r.Body.Close()

	req, status, errMsg := decodeChatRequest(r)
	if status != 0 {
		writeJSON(w, status, ErrorResponse{Error: errMsg})
		return
	}

	observability.L().InfoCtx(r.Context(), "Received chat stream request",
		"conversation_id", req.ConversationID,
		"user_id", req.UserID,
		"message_preview", truncateForLog(req.Message, 120),
		"mode", req.Mode,
		"handler", "ChatHandler.ChatStream",
	)

	ctx := r.Context()
	traceID := pkgctx.GenerateTraceID()
	ctx = pkgctx.WithTraceID(ctx, traceID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	resp, err := h.agentService.ChatStream(ctx, req, func(chunk string) error {
		return writeSSE(w, flusher, "delta", streamEvent{Content: chunk})
	})
	if err != nil || (resp != nil && resp.Error != "") {
		message := "chat stream failed"
		if resp != nil && resp.Error != "" {
			message = resp.Error
		} else if err != nil {
			message = err.Error()
		}
		observability.L().ErrorCtx(ctx, "Chat stream service error", nil,
			"error", message,
			"conversation_id", req.ConversationID,
			"handler", "ChatHandler.ChatStream",
		)
		_ = writeSSE(w, flusher, "error", streamEvent{Error: message})
		return
	}

	if resp == nil {
		_ = writeSSE(w, flusher, "error", streamEvent{Error: "empty chat response"})
		return
	}

	observability.L().InfoCtx(ctx, "Chat stream completed",
		"conversation_id", resp.ConversationID,
		"message_id", resp.MessageID,
		"mode", resp.Mode,
		"latency_ms", resp.LatencyMs,
		"handler", "ChatHandler.ChatStream",
	)

	_ = writeSSE(w, flusher, "done", resp)
}

func decodeChatRequest(r *http.Request) (*agent.ChatRequest, int, string) {
	var req agent.ChatRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		observability.L().ErrorCtx(r.Context(), "Failed to decode request body", err, "handler", "ChatHandler")
		return nil, http.StatusBadRequest, "invalid request body"
	}

	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		return nil, http.StatusBadRequest, "message is required"
	}

	if req.Mode != "" && !isValidChatMode(req.Mode) {
		return nil, http.StatusBadRequest, "invalid mode"
	}

	return &req, 0, ""
}
