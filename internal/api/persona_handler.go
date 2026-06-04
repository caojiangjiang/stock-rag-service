package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"stock_rag/internal/observability"
	"stock_rag/internal/persona/model"
	"stock_rag/internal/persona/service"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type PersonaHandler struct {
	personaService service.PersonaService
}

func NewPersonaHandler(personaService service.PersonaService) *PersonaHandler {
	return &PersonaHandler{personaService: personaService}
}

func (h *PersonaHandler) ListPersonas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		return
	}

	market := r.URL.Query().Get("market")
	styleTag := r.URL.Query().Get("style_tag")
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "active"
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	personas, err := h.personaService.ListPersonas(r.Context(), market, styleTag, status, limit)
	if err != nil {
		observability.L().ErrorCtx(r.Context(), "Failed to list personas", nil, "error", err.Error())
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to list personas"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": personas,
	})
}

func (h *PersonaHandler) GetPersona(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		return
	}

	personaID := r.PathValue("id")
	if personaID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "persona_id is required"})
		return
	}

	persona, err := h.personaService.GetPersona(r.Context(), personaID)
	if err != nil {
		if err == service.ErrPersonaNotFound {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "persona not found"})
			return
		}
		observability.L().ErrorCtx(r.Context(), "Failed to get persona", nil, "error", err.Error())
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to get persona"})
		return
	}

	writeJSON(w, http.StatusOK, persona)
}

func (h *PersonaHandler) Chat(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		return
	}

	defer r.Body.Close()

	var req model.PersonaChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		observability.L().ErrorCtx(r.Context(), "Persona chat invalid request body", nil,
			"feature", "persona",
			"mode", "chat",
			"error", err.Error(),
			"latency_ms", time.Since(startTime).Milliseconds())
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	if req.PersonaID == "" {
		observability.L().WarnCtx(r.Context(), "Persona chat missing persona_id",
			"feature", "persona",
			"mode", "chat",
			"latency_ms", time.Since(startTime).Milliseconds())
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "persona_id is required"})
		return
	}

	if req.Message == "" {
		observability.L().WarnCtx(r.Context(), "Persona chat missing message",
			"feature", "persona",
			"mode", "chat",
			"persona_id", req.PersonaID,
			"latency_ms", time.Since(startTime).Milliseconds())
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "message is required"})
		return
	}

	// 开启 tracing span
	ctx, span := observability.StartSpan(r.Context(), "persona.chat")
	defer span.End()

	// 设置 span 属性
	span.SetAttributes(
		attribute.String("persona_id", req.PersonaID),
		attribute.String("stock_code", req.StockCode),
		attribute.Int("question_length", len(req.Message)),
	)

	answer, err := h.personaService.Chat(ctx, &req)
	if err != nil {
		if err == service.ErrPersonaNotFound {
			span.SetStatus(codes.Error, "persona_not_found")
			observability.L().WarnCtx(ctx, "Persona chat persona not found",
				"feature", "persona",
				"mode", "chat",
				"persona_id", req.PersonaID,
				"latency_ms", time.Since(startTime).Milliseconds())
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "persona not found"})
			return
		}
		span.SetStatus(codes.Error, "chat_failed")
		observability.L().ErrorCtx(ctx, "Persona chat failed", nil,
			"feature", "persona",
			"mode", "chat",
			"persona_id", req.PersonaID,
			"error", err.Error(),
			"latency_ms", time.Since(startTime).Milliseconds())
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "chat failed"})
		return
	}

	span.SetAttributes(
		attribute.String("request_id", answer.RequestID),
		attribute.Int("citation_count", len(answer.Citations)),
	)

	observability.L().InfoCtx(ctx, "Persona chat handler completed",
		"feature", "persona",
		"mode", "chat",
		"persona_id", req.PersonaID,
		"request_id", answer.RequestID,
		"latency_ms", time.Since(startTime).Milliseconds())

	writeJSON(w, http.StatusOK, answer)
}

func (h *PersonaHandler) Roundtable(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		return
	}

	defer r.Body.Close()

	var req model.RoundtableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		observability.L().ErrorCtx(r.Context(), "Roundtable invalid request body", nil,
			"feature", "persona",
			"mode", "roundtable",
			"error", err.Error(),
			"latency_ms", time.Since(startTime).Milliseconds())
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	if req.Question == "" {
		observability.L().WarnCtx(r.Context(), "Roundtable missing question",
			"feature", "persona",
			"mode", "roundtable",
			"latency_ms", time.Since(startTime).Milliseconds())
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "question is required"})
		return
	}

	// 如果未传 persona_ids，使用默认组合（美股成长 + 美股价值 + A股防御）
	if len(req.PersonaIDs) == 0 {
		req.PersonaIDs = []string{"us_growth_tech", "us_value_recovery", "cn_dividend_defensive"}
	}

	if len(req.PersonaIDs) < 2 {
		observability.L().WarnCtx(r.Context(), "Roundtable insufficient personas",
			"feature", "persona",
			"mode", "roundtable",
			"participant_count", len(req.PersonaIDs),
			"latency_ms", time.Since(startTime).Milliseconds())
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "at least 2 personas required"})
		return
	}

	if len(req.PersonaIDs) > 5 {
		observability.L().WarnCtx(r.Context(), "Roundtable too many personas",
			"feature", "persona",
			"mode", "roundtable",
			"participant_count", len(req.PersonaIDs),
			"latency_ms", time.Since(startTime).Milliseconds())
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "max 5 personas allowed"})
		return
	}

	// 开启 tracing span
	ctx, span := observability.StartSpan(r.Context(), "persona.roundtable")
	defer span.End()

	// 设置 span 属性
	span.SetAttributes(
		attribute.Int("participant_count", len(req.PersonaIDs)),
		attribute.String("participant_ids", strings.Join(req.PersonaIDs, ",")),
		attribute.Int("question_length", len(req.Question)),
	)

	response, err := h.personaService.Roundtable(ctx, &req)
	if err != nil {
		span.SetStatus(codes.Error, "roundtable_failed")
		observability.L().ErrorCtx(ctx, "Roundtable failed", nil,
			"feature", "persona",
			"mode", "roundtable",
			"participant_count", len(req.PersonaIDs),
			"error", err.Error(),
			"latency_ms", time.Since(startTime).Milliseconds())
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "roundtable failed"})
		return
	}

	span.SetAttributes(
		attribute.String("request_id", response.RequestID),
		attribute.Int("consensus_count", len(response.Consensus)),
		attribute.Int("disagreements_count", len(response.Disagreements)),
		attribute.Int("risk_focus_count", len(response.RiskFocus)),
	)

	observability.L().InfoCtx(ctx, "Roundtable handler completed",
		"feature", "persona",
		"mode", "roundtable",
		"participant_count", len(req.PersonaIDs),
		"request_id", response.RequestID,
		"consensus_count", len(response.Consensus),
		"disagreements_count", len(response.Disagreements),
		"risk_focus_count", len(response.RiskFocus),
		"latency_ms", time.Since(startTime).Milliseconds())

	writeJSON(w, http.StatusOK, response)
}
