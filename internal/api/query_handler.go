package api

import (
	"encoding/json"
	"net/http"

	appmodel "stock_rag/internal/model"
)

// QueryHandlerPlan 记录后续 query handler 需要接入的能力。
type QueryHandlerPlan struct {
	NeedValidation bool
	NeedSSE        bool
	NeedCitations  bool
	NeedRequestID  bool
}

// DefaultQueryHandlerPlan 返回第一版 handler 设计目标。
func DefaultQueryHandlerPlan() QueryHandlerPlan {
	return QueryHandlerPlan{
		NeedValidation: true,
		NeedSSE:        true,
		NeedCitations:  true,
		NeedRequestID:  true,
	}
}

// QueryHandler 返回 /rag/query 对应的 HTTP handler。
func QueryHandler(svc QueryService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		if svc == nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "query service unavailable"})
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

		resp, err := svc.Query(r.Context(), req)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "query failed"})
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}
