package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"stock_rag/internal/persona/service"
)

func TestPersonaHandlerListPersonas(t *testing.T) {
	handler := NewPersonaHandler(service.NewMockPersonaService())

	req := httptest.NewRequest(http.MethodGet, "/api/personas", nil)
	resp := httptest.NewRecorder()

	handler.ListPersonas(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.Code)
	}

	var result map[string]interface{}
	if err := decodeJSON(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	items, ok := result["items"].([]interface{})
	if !ok {
		t.Fatal("expected items array")
	}

	if len(items) < 1 {
		t.Fatal("expected at least 1 persona")
	}
}

func TestPersonaHandlerGetPersona(t *testing.T) {
	handler := NewPersonaHandler(service.NewMockPersonaService())

	req := httptest.NewRequest(http.MethodGet, "/api/personas/us_growth_tech", nil)
	req.SetPathValue("id", "us_growth_tech")
	resp := httptest.NewRecorder()

	handler.GetPersona(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.Code)
	}
}

func TestPersonaHandlerGetPersonaNotFound(t *testing.T) {
	handler := NewPersonaHandler(service.NewMockPersonaService())

	req := httptest.NewRequest(http.MethodGet, "/api/personas/nonexistent", nil)
	req.SetPathValue("id", "nonexistent")
	resp := httptest.NewRecorder()

	handler.GetPersona(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, resp.Code)
	}
}

func TestPersonaHandlerChat(t *testing.T) {
	handler := NewPersonaHandler(service.NewMockPersonaService())

	body := bytes.NewBufferString(`{"persona_id":"us_growth_tech","message":"test message"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/personas/chat", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	handler.Chat(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.Code)
	}
}

func TestPersonaHandlerChatMissingPersonaID(t *testing.T) {
	handler := NewPersonaHandler(service.NewMockPersonaService())

	body := bytes.NewBufferString(`{"message":"test message"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/personas/chat", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	handler.Chat(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.Code)
	}
}

func TestPersonaHandlerRoundtable(t *testing.T) {
	handler := NewPersonaHandler(service.NewMockPersonaService())

	body := bytes.NewBufferString(`{"question":"test question"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/personas/roundtable", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	handler.Roundtable(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.Code)
	}
}

func TestPersonaHandlerRoundtableTooFewPersonas(t *testing.T) {
	handler := NewPersonaHandler(service.NewMockPersonaService())

	body := bytes.NewBufferString(`{"question":"test question","persona_ids":["one"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/personas/roundtable", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	handler.Roundtable(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.Code)
	}
}

func TestPersonaHandlerMethodNotAllowed(t *testing.T) {
	handler := NewPersonaHandler(service.NewMockPersonaService())

	req := httptest.NewRequest(http.MethodPut, "/api/personas", nil)
	resp := httptest.NewRecorder()
	handler.ListPersonas(resp, req)

	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, resp.Code)
	}
}

func decodeJSON(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
