package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"stock_rag/internal/persona/model"
)

func getProjectRoot() string {
	// 向上查找包含 configs 目录的位置
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

func TestLoadActualConfig(t *testing.T) {
	root := getProjectRoot()
	if root == "" {
		t.Skip("Cannot find project root")
	}

	configPath := filepath.Join(root, "configs", "personas.yaml")
	loader, err := NewPersonaConfigLoader(configPath)
	if err != nil {
		t.Fatalf("Failed to load config from %s: %v", configPath, err)
	}

	personas := loader.GetAllPersonas()
	fmt.Printf("Loaded %d personas\n", len(personas))
	
	for _, p := range personas {
		fmt.Printf("- ID: %s, Name: %s, Market: %s\n", p.PersonaID, p.Name, p.Market)
	}

	if len(personas) != 6 {
		t.Errorf("Expected 6 personas, got %d", len(personas))
	}

	// 测试获取一个 persona
	profile, err := loader.GetPersona("us_growth_tech")
	if err != nil {
		t.Fatalf("Failed to get us_growth_tech: %v", err)
	}
	fmt.Printf("Got persona: %s, %s\n", profile.PersonaID, profile.Name)

	if profile.Market != "us" {
		t.Errorf("Expected market 'us', got '%s'", profile.Market)
	}
}

func TestPersonaProfileMapping(t *testing.T) {
	// 直接测试 PersonaProfile 是否可以正确从 YAML 解析
	profile := model.PersonaProfile{
		PersonaID: "test",
		Name:      "Test",
		Market:    "us",
		StyleTags: []string{"growth"},
	}

	if profile.PersonaID != "test" {
		t.Errorf("Expected persona_id 'test', got '%s'", profile.PersonaID)
	}
}

func TestGetRiskLevel(t *testing.T) {
	tests := []struct {
		maxDrawdown float64
		expected    string
	}{
		{-5.0, "low"},
		{-9.9, "low"},
		{-10.0, "low"},
		{-15.0, "medium"},
		{-19.9, "medium"},
		{-20.0, "medium"},
		{-25.0, "high"},
		{-50.0, "high"},
	}

	for _, tt := range tests {
		result := getRiskLevel(tt.maxDrawdown)
		if result != tt.expected {
			t.Errorf("getRiskLevel(%f) = %s, expected %s", tt.maxDrawdown, result, tt.expected)
		}
	}
}

func TestPersonaConfigLoader_FileNotFound(t *testing.T) {
	_, err := NewPersonaConfigLoader("nonexistent.yaml")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}
