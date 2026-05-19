package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	appmodel "stock_rag/internal/model"
)

type streamEvent struct {
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

// QueryStreamHandler 返回 /rag/query/stream 对应的 SSE handler。
func QueryStreamHandler(svc QueryService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		if svc == nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "query service unavailable"})
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "streaming unsupported"})
			return
		}

		defer r.Body.Close()

		var req appmodel.RAGQueryRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		resp, err := svc.QueryStream(r.Context(), req, func(chunk string) error {
			return writeSSE(w, flusher, "delta", streamEvent{Content: chunk})
		})
		if err != nil {
			_ = writeSSE(w, flusher, "error", streamEvent{Error: "query stream failed"})
			return
		}

		_ = writeSSE(w, flusher, "done", resp)
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}
