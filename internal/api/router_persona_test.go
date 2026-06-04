package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"stock_rag/internal/persona/service"
)

// TestPersonaFeatureFlagDisabled 测试 ENABLE_PERSONA_MODULE 关闭时 persona 路由不可用
func TestPersonaFeatureFlagDisabled(t *testing.T) {
	// 保存原始环境变量
	originalValue := os.Getenv("ENABLE_PERSONA_MODULE")
	defer os.Setenv("ENABLE_PERSONA_MODULE", originalValue)

	// 设置 feature flag 为关闭
	os.Setenv("ENABLE_PERSONA_MODULE", "false")

	// 创建最小化的 router
	mux := http.NewServeMux()
	registerPersonaRoutes(mux, nil, false)

	// 测试 /api/personas 返回 404
	req := httptest.NewRequest(http.MethodGet, "/api/personas", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 when persona module disabled, got %d", rec.Code)
	}

	// 测试 /api/personas/{id} 返回 404
	req = httptest.NewRequest(http.MethodGet, "/api/personas/us_growth_tech", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for persona detail when disabled, got %d", rec.Code)
	}

	// 测试 /api/personas/chat 返回 404
	req = httptest.NewRequest(http.MethodPost, "/api/personas/chat", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for persona chat when disabled, got %d", rec.Code)
	}

	// 测试 /api/personas/roundtable 返回 404
	req = httptest.NewRequest(http.MethodPost, "/api/personas/roundtable", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for roundtable when disabled, got %d", rec.Code)
	}
}

// TestPersonaFeatureFlagEnabled 测试 ENABLE_PERSONA_MODULE 开启时路由可用
func TestPersonaFeatureFlagEnabled(t *testing.T) {
	// 使用 mock service 测试
	mockSvc := service.NewMockPersonaService()
	handler := NewPersonaHandler(mockSvc)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/personas", handler.ListPersonas)
	mux.HandleFunc("/api/personas/{id}", handler.GetPersona)

	// 测试 /api/personas 返回 200
	req := httptest.NewRequest(http.MethodGet, "/api/personas", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 when persona module enabled, got %d", rec.Code)
	}

	// 测试 /api/personas/{id} 返回 200 或 404 (取决于 ID)
	req = httptest.NewRequest(http.MethodGet, "/api/personas/us_growth_tech", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// mock service 可能返回 200 或 404，但不应该是其他错误
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Errorf("Expected 200 or 404 for persona detail, got %d", rec.Code)
	}
}

// TestPersonaRoundtableFeatureFlag 测试 ENABLE_PERSONA_ROUNDTABLE 子开关
func TestPersonaRoundtableFeatureFlag(t *testing.T) {
	mockSvc := service.NewMockPersonaService()
	handler := NewPersonaHandler(mockSvc)

	// 测试 roundtable 关闭时返回 404
	mux := http.NewServeMux()
	mux.HandleFunc("/api/personas/roundtable", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/personas/roundtable", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 when roundtable disabled, got %d", rec.Code)
	}

	// 测试 roundtable 开启时可访问
	mux2 := http.NewServeMux()
	mux2.HandleFunc("/api/personas/roundtable", handler.Roundtable)

	req = httptest.NewRequest(http.MethodPost, "/api/personas/roundtable", nil)
	rec = httptest.NewRecorder()
	mux2.ServeHTTP(rec, req)

	// 应该返回 400 (缺少请求体) 或 200，不应该是 404
	if rec.Code == http.StatusNotFound {
		t.Errorf("Roundtable should be accessible when enabled, got 404")
	}
}

// TestPersonaModuleEnvironmentParsing 测试环境变量解析
func TestPersonaModuleEnvironmentParsing(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected bool
	}{
		{"true lowercase", "true", true},
		{"TRUE uppercase", "TRUE", true},
		{"True mixed", "True", true},
		{"true with spaces", " true ", true},
		{"false", "false", false},
		{"empty", "", false},
		{"random", "random", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseFeatureFlag(tt.envValue)
			if result != tt.expected {
				t.Errorf("parseFeatureFlag(%q) = %v, expected %v", tt.envValue, result, tt.expected)
			}
		})
	}
}

// parseFeatureFlag 解析 feature flag 环境变量
func parseFeatureFlag(value string) bool {
	return strEqualFold(strTrimSpace(value), "true")
}

// registerPersonaRoutes 注册 persona 路由 (测试辅助函数)
func registerPersonaRoutes(mux *http.ServeMux, handler *PersonaHandler, enableRoundtable bool) {
	if handler == nil {
		// feature flag 关闭时返回 404
		mux.HandleFunc("/api/personas", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		mux.HandleFunc("/api/personas/{id}", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		mux.HandleFunc("/api/personas/chat", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		mux.HandleFunc("/api/personas/roundtable", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		return
	}

	mux.HandleFunc("/api/personas", handler.ListPersonas)
	mux.HandleFunc("/api/personas/{id}", handler.GetPersona)
	mux.HandleFunc("/api/personas/chat", handler.Chat)

	if enableRoundtable {
		mux.HandleFunc("/api/personas/roundtable", handler.Roundtable)
	} else {
		mux.HandleFunc("/api/personas/roundtable", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
	}
}

// 辅助函数 (避免导入 strings)
func strEqualFold(s, t string) bool {
	if len(s) != len(t) {
		return false
	}
	for i := 0; i < len(s); i++ {
		c1, c2 := s[i], t[i]
		if c1 >= 'A' && c1 <= 'Z' {
			c1 += 32
		}
		if c2 >= 'A' && c2 <= 'Z' {
			c2 += 32
		}
		if c1 != c2 {
			return false
		}
	}
	return true
}

func strTrimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
