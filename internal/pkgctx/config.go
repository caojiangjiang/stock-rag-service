package pkgctx

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	MySQL    MySQLConfig    `yaml:"mysql"`
	Redis    RedisConfig    `yaml:"redis"`
	Postgres PostgresConfig `yaml:"postgres"`
}

// MySQLConfig MySQL 配置
type MySQLConfig struct {
	Host     string `yaml:"host"`
	Port     string `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
}

// RedisConfig Redis 配置
type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     string `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// PostgresConfig PostgreSQL 配置
type PostgresConfig struct {
	Host     string `yaml:"host"`
	Port     string `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
	SSLMode  string `yaml:"sslmode"`
}

// RAGConfig RAG 配置
type RAGConfig struct {
	TopK             int  `yaml:"top_k"`
	MaxContextChunks int  `yaml:"max_context_chunks"`
	EnableStream     bool `yaml:"enable_stream"`
}

// ModelConfig 模型配置
type ModelConfig struct {
	Provider  string `yaml:"provider"`
	Name      string `yaml:"name"`
	APIKeyEnv string `yaml:"api_key_env"`
	ModelEnv  string `yaml:"model_env"`
}

// RetrieverConfig 检索器配置
type RetrieverConfig struct {
	VectorStore     string   `yaml:"vector_store"`
	DefaultDocTypes []string `yaml:"default_doc_types"`
}

// LLMConfig LLM 配置
type LLMConfig struct {
	MaxQueueSize   int `yaml:"max_queue_size"`
	MaxConcurrency int `yaml:"max_concurrency"`
	MaxWaitTimeMs  int `yaml:"max_wait_time_ms"` // 最大等待时长（毫秒），0 表示不限制
}

// AppConfig 是应用配置。
type AppConfig struct {
	AppName      string             `yaml:"app_name"`
	HTTP         HTTPConfig         `yaml:"http"`
	Database     DatabaseConfig     `yaml:"database"`
	RAG          RAGConfig          `yaml:"rag"`
	Model        ModelConfig        `yaml:"model"`
	Retriever    RetrieverConfig    `yaml:"retriever"`
	LLM          LLMConfig          `yaml:"llm"`
	Embeddingder EmbeddingderConfig `yaml:"embeddingder"`
	Executor     ExecutorStrategyConfig `yaml:"executor"`
}

// EmbeddingderConfig 嵌入器配置
type EmbeddingderConfig struct {
	Model     string `yaml:"model"`
	APIKeyEnv string `yaml:"api_key_env"`
}

// HTTPConfig HTTP 配置
type HTTPConfig struct {
	Port int `yaml:"port"`
}

// DefaultConfig 返回项目默认配置。
func DefaultConfig() AppConfig {
	return AppConfig{
		AppName: "stock_rag",
		HTTP: HTTPConfig{
			Port: 8080,
		},
		Database: DatabaseConfig{
			MySQL: MySQLConfig{
				Host:     "localhost",
				Port:     "3306",
				User:     "root",
				Password: "root123456",
				Database: "stock_rag",
			},
			Redis: RedisConfig{
				Host:     "localhost",
				Port:     "6379",
				Password: "redis123456",
				DB:       0,
			},
			Postgres: PostgresConfig{
				Host:     "localhost",
				Port:     "5432",
				User:     "postgres",
				Password: "postgres123456",
				Database: "stock_rag",
				SSLMode:  "disable",
			},
		},
		RAG: RAGConfig{
			TopK:             5,
			MaxContextChunks: 6,
			EnableStream:     true,
		},
		Model: ModelConfig{
			Provider:  "ark",
			Name:      "",
			APIKeyEnv: "ARK_API_KEY",
			ModelEnv:  "ARK_MODEL",
		},
		Retriever: RetrieverConfig{
			VectorStore: "pgvector",
			DefaultDocTypes: []string{
				"announcement",
				"report",
				"news",
			},
		},
		LLM: LLMConfig{
			MaxQueueSize:   100,
			MaxConcurrency: 10,
			MaxWaitTimeMs:  30000, // 默认最大等待 30 秒
		},
	}
}

// LoadConfig 从文件加载配置
func LoadConfig(configPath string) (AppConfig, error) {
	config := DefaultConfig()

	// 检查文件是否存在
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return config, fmt.Errorf("config file not found: %s", configPath)
	}

	// 读取文件内容
	data, err := os.ReadFile(configPath)
	if err != nil {
		return config, fmt.Errorf("failed to read config file: %v", err)
	}

	// 解析 YAML
	if err := yaml.Unmarshal(data, &config); err != nil {
		return config, fmt.Errorf("failed to parse config file: %v", err)
	}

	return config, nil
}
