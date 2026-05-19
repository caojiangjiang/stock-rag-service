package pkgctx

type ExecutorStrategyConfig struct {
	MaxSteps          int                `yaml:"max_steps"`
	MaxToolCalls      int                `yaml:"max_tool_calls"`
	MaxTokenBudget    int                `yaml:"max_token_budget"`
	ToolTimeoutMs     int                `yaml:"tool_timeout_ms"`
	SubAgentTimeoutMs int                `yaml:"sub_agent_timeout_ms"`
	MaxSubAgents      int                `yaml:"max_sub_agents"`
	EnableFallback    bool               `yaml:"enable_fallback"`
	FallbackStrategy  FallbackStrategy   `yaml:"fallback_strategy"`
	RetryPolicy       RetryPolicyConfig `yaml:"retry_policy"`
	FeatureFlags      FeatureFlagsConfig `yaml:"feature_flags"`
}

type FallbackStrategy string

const (
	FallbackToSimple   FallbackStrategy = "simple"
	FallbackToDirect   FallbackStrategy = "direct"
	FallbackToEmpty     FallbackStrategy = "empty"
)

type RetryPolicyConfig struct {
	MaxRetries       int `yaml:"max_retries"`
	InitialDelayMs   int `yaml:"initial_delay_ms"`
	MaxDelayMs       int `yaml:"max_delay_ms"`
	BackoffMultiplier float64 `yaml:"backoff_multiplier"`
}

type FeatureFlagsConfig struct {
	EnableTypedTools     bool `yaml:"enable_typed_tools"`
	EnableCheckpoint     bool `yaml:"enable_checkpoint"`
	EnableStreaming      bool `yaml:"enable_streaming"`
	EnableObservability  bool `yaml:"enable_observability"`
	EnableAuditLog       bool `yaml:"enable_audit_log"`
	EnableToolValidation bool `yaml:"enable_tool_validation"`
	EnableOutputContract bool `yaml:"enable_output_contract"`
}

func DefaultExecutorStrategy() ExecutorStrategyConfig {
	return ExecutorStrategyConfig{
		MaxSteps:          50,
		MaxToolCalls:      100,
		MaxTokenBudget:    8000,
		ToolTimeoutMs:     30000,
		SubAgentTimeoutMs: 120000,
		MaxSubAgents:      5,
		EnableFallback:    true,
		FallbackStrategy:  FallbackToSimple,
		RetryPolicy: RetryPolicyConfig{
			MaxRetries:        3,
			InitialDelayMs:   100,
			MaxDelayMs:       5000,
			BackoffMultiplier: 2.0,
		},
		FeatureFlags: FeatureFlagsConfig{
			EnableTypedTools:     true,
			EnableCheckpoint:     true,
			EnableStreaming:      true,
			EnableObservability: true,
			EnableAuditLog:       true,
			EnableToolValidation: true,
			EnableOutputContract: true,
		},
	}
}

func (c *ExecutorStrategyConfig) Validate() error {
	if c.MaxSteps <= 0 {
		c.MaxSteps = 50
	}
	if c.MaxToolCalls <= 0 {
		c.MaxToolCalls = 100
	}
	if c.ToolTimeoutMs <= 0 {
		c.ToolTimeoutMs = 30000
	}
	if c.SubAgentTimeoutMs <= 0 {
		c.SubAgentTimeoutMs = 120000
	}
	if c.MaxSubAgents <= 0 {
		c.MaxSubAgents = 5
	}
	return nil
}

func (c *AppConfig) GetExecutorStrategy() ExecutorStrategyConfig {
	if c.Executor == (ExecutorStrategyConfig{}) {
		return DefaultExecutorStrategy()
	}
	cfg := c.Executor
	cfg.Validate()
	return cfg
}