package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"stock_rag/internal/persona/service"
)

// 集成测试：验证 handler + config service + config loader 完整链路

func getProjectRootForIntegration() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "configs")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func TestPersonaHandlerWithConfigService_ListPersonas(t *testing.T) {
	root := getProjectRootForIntegration()
	if root == "" {
		t.Skip("Cannot find project root")
	}

	configPath := filepath.Join(root, "configs", "personas.yaml")
	configSvc, err := service.NewConfigPersonaServiceWithDefaults(configPath)
	if err != nil {
		t.Fatalf("Failed to create config service: %v", err)
	}

	handler := NewPersonaHandler(configSvc)

	// 测试获取所有 personas
	req := httptest.NewRequest(http.MethodGet, "/api/personas", nil)
	rec := httptest.NewRecorder()
	handler.ListPersonas(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}

	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	items, ok := response["items"].([]any)
	if !ok {
		t.Fatal("Expected items array in response")
	}
	if len(items) != 6 {
		t.Errorf("Expected 6 personas, got %d", len(items))
	}

	// 验证第一个 persona 的字段
	if len(items) > 0 {
		first := items[0].(map[string]any)
		if first["persona_id"] == "" {
			t.Error("Expected non-empty persona_id")
		}
		if first["name"] == "" {
			t.Error("Expected non-empty name")
		}
	}
}

func TestPersonaHandlerWithConfigService_FilterByMarket(t *testing.T) {
	root := getProjectRootForIntegration()
	if root == "" {
		t.Skip("Cannot find project root")
	}

	configPath := filepath.Join(root, "configs", "personas.yaml")
	configSvc, err := service.NewConfigPersonaServiceWithDefaults(configPath)
	if err != nil {
		t.Fatalf("Failed to create config service: %v", err)
	}

	handler := NewPersonaHandler(configSvc)

	// 测试 market 过滤
	req := httptest.NewRequest(http.MethodGet, "/api/personas?market=us", nil)
	rec := httptest.NewRecorder()
	handler.ListPersonas(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}

	var response map[string]any
	json.Unmarshal(rec.Body.Bytes(), &response)
	items := response["items"].([]any)

	if len(items) != 3 {
		t.Errorf("Expected 3 US personas, got %d", len(items))
	}

	// 验证所有返回的 persona 都是 US 市场
	for _, item := range items {
		p := item.(map[string]any)
		if p["market"] != "us" {
			t.Errorf("Expected market 'us', got '%v'", p["market"])
		}
	}
}

func TestPersonaHandlerWithConfigService_GetPersona(t *testing.T) {
	root := getProjectRootForIntegration()
	if root == "" {
		t.Skip("Cannot find project root")
	}

	configPath := filepath.Join(root, "configs", "personas.yaml")
	configSvc, err := service.NewConfigPersonaServiceWithDefaults(configPath)
	if err != nil {
		t.Fatalf("Failed to create config service: %v", err)
	}

	handler := NewPersonaHandler(configSvc)

	// 测试获取存在的 persona
	req := httptest.NewRequest(http.MethodGet, "/api/personas/us_growth_tech", nil)
	req.SetPathValue("id", "us_growth_tech")
	rec := httptest.NewRecorder()
	handler.GetPersona(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}

	var profile map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if profile["persona_id"] != "us_growth_tech" {
		t.Errorf("Expected persona_id 'us_growth_tech', got '%v'", profile["persona_id"])
	}
	if profile["name"] != "美股科技成长派" {
		t.Errorf("Expected name '美股科技成长派', got '%v'", profile["name"])
	}
	if profile["market"] != "us" {
		t.Errorf("Expected market 'us', got '%v'", profile["market"])
	}

	// 验证关键字段存在
	requiredFields := []string{"investment_belief", "preferred_sectors", "style_tags", "performance"}
	for _, field := range requiredFields {
		if _, ok := profile[field]; !ok {
			t.Errorf("Missing required field: %s", field)
		}
	}
}

func TestPersonaHandlerWithConfigService_GetPersonaNotFound(t *testing.T) {
	root := getProjectRootForIntegration()
	if root == "" {
		t.Skip("Cannot find project root")
	}

	configPath := filepath.Join(root, "configs", "personas.yaml")
	configSvc, err := service.NewConfigPersonaServiceWithDefaults(configPath)
	if err != nil {
		t.Fatalf("Failed to create config service: %v", err)
	}

	handler := NewPersonaHandler(configSvc)

	// 测试获取不存在的 persona
	req := httptest.NewRequest(http.MethodGet, "/api/personas/nonexistent", nil)
	req.SetPathValue("id", "nonexistent")
	rec := httptest.NewRecorder()
	handler.GetPersona(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", rec.Code)
	}
}

func TestPersonaHandlerWithConfigService_Chat(t *testing.T) {
	root := getProjectRootForIntegration()
	if root == "" {
		t.Skip("Cannot find project root")
	}

	configPath := filepath.Join(root, "configs", "personas.yaml")
	configSvc, err := service.NewConfigPersonaServiceWithDefaults(configPath)
	if err != nil {
		t.Fatalf("Failed to create config service: %v", err)
	}

	handler := NewPersonaHandler(configSvc)

	// 测试 chat
	body := map[string]any{
		"persona_id": "us_growth_tech",
		"message":    "AI龙头还能涨吗？",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/personas/chat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.Chat(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}

	var answer map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if answer["persona_id"] != "us_growth_tech" {
		t.Errorf("Expected persona_id 'us_growth_tech', got '%v'", answer["persona_id"])
	}
	if answer["persona_name"] != "美股科技成长派" {
		t.Errorf("Expected persona_name '美股科技成长派', got '%v'", answer["persona_name"])
	}
}

func TestPersonaHandlerWithConfigService_Roundtable(t *testing.T) {
	root := getProjectRootForIntegration()
	if root == "" {
		t.Skip("Cannot find project root")
	}

	configPath := filepath.Join(root, "configs", "personas.yaml")
	configSvc, err := service.NewConfigPersonaServiceWithDefaults(configPath)
	if err != nil {
		t.Fatalf("Failed to create config service: %v", err)
	}

	handler := NewPersonaHandler(configSvc)

	// 测试 roundtable
	body := map[string]any{
		"question":    "当前市场怎么看？",
		"persona_ids": []string{"us_growth_tech", "cn_dividend_defensive"},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/personas/roundtable", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.Roundtable(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response["question"] != "当前市场怎么看？" {
		t.Errorf("Expected question, got '%v'", response["question"])
	}

	participants := response["participants"].([]any)
	if len(participants) != 2 {
		t.Errorf("Expected 2 participants, got %d", len(participants))
	}

	answers := response["answers"].([]any)
	if len(answers) != 2 {
		t.Errorf("Expected 2 answers, got %d", len(answers))
	}
}
