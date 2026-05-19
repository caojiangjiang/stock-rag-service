package api

import "net/http"

// DocumentsListHandler 返回 /documents 对应的 HTTP handler。
func DocumentsListHandler(svc QueryService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		if svc == nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "query service unavailable"})
			return
		}

		resp, err := svc.ListDocuments(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}
