package api

import (
	"encoding/json"
	"net/http"

	appmodel "stock_rag/internal/model"
)

// DocumentsImportHandler 返回 /documents/import 对应的 HTTP handler。
func DocumentsImportHandler(svc QueryService) http.HandlerFunc {
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

		var req appmodel.DocumentsImportRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}

		resp, err := svc.ImportDocuments(r.Context(), req)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}
