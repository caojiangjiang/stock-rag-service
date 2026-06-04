package agent

import (
	"fmt"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// CoordinatorRuleConfig YAML 配置结构
type CoordinatorRuleConfig struct {
	Rules []CoordinatorRule `yaml:"rules"`
}

// ConfigurableCoordinatorRuleMatcher 支持配置加载和热更新的规则匹配器
type ConfigurableCoordinatorRuleMatcher struct {
	rules     []CoordinatorRule
	rulesLock sync.RWMutex
	configPath string
	lastLoad   time.Time
}

// NewConfigurableCoordinatorRuleMatcher 创建可配置的规则匹配器
func NewConfigurableCoordinatorRuleMatcher(configPath string) (*ConfigurableCoordinatorRuleMatcher, error) {
	matcher := &ConfigurableCoordinatorRuleMatcher{
		configPath: configPath,
	}
	if err := matcher.LoadRules(); err != nil {
		return nil, err
	}
	return matcher, nil
}

// LoadRules 从配置文件加载规则
func (m *ConfigurableCoordinatorRuleMatcher) LoadRules() error {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var config CoordinatorRuleConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to unmarshal YAML: %w", err)
	}

	m.rulesLock.Lock()
	m.rules = config.Rules
	m.lastLoad = time.Now()
	m.rulesLock.Unlock()

	return nil
}

// ReloadRules 热更新规则（线程安全）
func (m *ConfigurableCoordinatorRuleMatcher) ReloadRules() error {
	return m.LoadRules()
}

// GetRules 获取当前规则（线程安全）
func (m *ConfigurableCoordinatorRuleMatcher) GetRules() []CoordinatorRule {
	m.rulesLock.RLock()
	defer m.rulesLock.RUnlock()
	return append([]CoordinatorRule(nil), m.rules...)
}

// GetLastLoadTime 获取最后加载时间
func (m *ConfigurableCoordinatorRuleMatcher) GetLastLoadTime() time.Time {
	m.rulesLock.RLock()
	defer m.rulesLock.RUnlock()
	return m.lastLoad
}

// Match 执行规则匹配
func (m *ConfigurableCoordinatorRuleMatcher) Match(input *CoordinatorSelectInput, complexity float64) ([]CoordinatorRuleMatch, error) {
	m.rulesLock.RLock()
	rules := m.rules
	m.rulesLock.RUnlock()

	var matches []CoordinatorRuleMatch
	for _, rule := range rules {
		if complexity < rule.MinComplexity {
			continue
		}
		if containsAny(input.CurrentMessage, rule.Keywords) {
			matches = append(matches, CoordinatorRuleMatch{
				Type:       rule.Type,
				Confidence: rule.Confidence,
				Reason:     rule.Reason,
				RuleName:   rule.Name,
			})
		}
	}
	return matches, nil
}

// NewDefaultConfigurableCoordinatorRuleMatcher 使用默认配置路径创建匹配器
func NewDefaultConfigurableCoordinatorRuleMatcher() (*ConfigurableCoordinatorRuleMatcher, error) {
	return NewConfigurableCoordinatorRuleMatcher("configs/coordinator_rules.yaml")
}