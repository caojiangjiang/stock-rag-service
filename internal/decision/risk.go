package decision

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"stock_rag/internal/portfolio"
)

// RiskRules 组合风险规则配置。
type RiskRules struct {
	Rules struct {
		MaxSinglePositionWeightPct float64  `yaml:"max_single_position_weight_pct"`
		MaxThemeWeightPct          float64  `yaml:"max_theme_weight_pct"`
		RequireThesisForStock      bool     `yaml:"require_thesis_for_stock"`
		MinThesisLength            int      `yaml:"min_thesis_length"`
		RequireFalsificationForStock bool   `yaml:"require_falsification_for_stock"`
		MinFalsificationLength     int      `yaml:"min_falsification_length"`
		WarnThemeStates            []string `yaml:"warn_theme_states"`
		WarnUnrealizedLossPct      float64  `yaml:"warn_unrealized_loss_pct"`
	} `yaml:"rules"`
}

func LoadRiskRules(path string) (*RiskRules, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read risk rules: %w", err)
	}
	var cfg RiskRules
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse risk rules: %w", err)
	}
	applyRiskDefaults(&cfg)
	return &cfg, nil
}

func applyRiskDefaults(cfg *RiskRules) {
	if cfg.Rules.MaxSinglePositionWeightPct <= 0 {
		cfg.Rules.MaxSinglePositionWeightPct = 25
	}
	if cfg.Rules.MaxThemeWeightPct <= 0 {
		cfg.Rules.MaxThemeWeightPct = 40
	}
	if cfg.Rules.MinThesisLength <= 0 {
		cfg.Rules.MinThesisLength = 8
	}
	if cfg.Rules.MinFalsificationLength <= 0 {
		cfg.Rules.MinFalsificationLength = 6
	}
	if len(cfg.Rules.WarnThemeStates) == 0 {
		cfg.Rules.WarnThemeStates = []string{"cooling", "overheated"}
	}
	if cfg.Rules.WarnUnrealizedLossPct <= 0 {
		cfg.Rules.WarnUnrealizedLossPct = 15
	}
}

func (cfg *RiskRules) Evaluate(summary *portfolio.Summary, exposures []ThemeExposure) RiskReport {
	if cfg == nil || summary == nil {
		return RiskReport{Passed: true}
	}
	var violations []RiskViolation
	warnStates := make(map[string]struct{}, len(cfg.Rules.WarnThemeStates))
	for _, s := range cfg.Rules.WarnThemeStates {
		warnStates[strings.ToLower(strings.TrimSpace(s))] = struct{}{}
	}

	for _, p := range summary.Positions {
		if p.HasQuote && p.WeightPct > cfg.Rules.MaxSinglePositionWeightPct {
			violations = append(violations, RiskViolation{
				RuleID:   "max_single_position_weight",
				Severity: "warn",
				Message:  fmt.Sprintf("%s 仓位 %.1f%% 超过上限 %.0f%%", label(p), p.WeightPct, cfg.Rules.MaxSinglePositionWeightPct),
				Symbol:   p.StockCode,
				Value:    p.WeightPct,
				Limit:    cfg.Rules.MaxSinglePositionWeightPct,
			})
		}
		if normalizeAsset(p.AssetType) == portfolio.AssetStock && cfg.Rules.RequireThesisForStock {
			if len(strings.TrimSpace(p.Thesis)) < cfg.Rules.MinThesisLength {
				violations = append(violations, RiskViolation{
					RuleID:   "missing_thesis",
					Severity: "warn",
					Message:  fmt.Sprintf("%s 缺少买入逻辑（至少 %d 字）", label(p), cfg.Rules.MinThesisLength),
					Symbol:   p.StockCode,
				})
			}
		}
		if normalizeAsset(p.AssetType) == portfolio.AssetStock && cfg.Rules.RequireFalsificationForStock {
			if len(strings.TrimSpace(p.Falsification)) < cfg.Rules.MinFalsificationLength {
				violations = append(violations, RiskViolation{
					RuleID:   "missing_falsification",
					Severity: "warn",
					Message:  fmt.Sprintf("%s 缺少证伪条件（至少 %d 字）", label(p), cfg.Rules.MinFalsificationLength),
					Symbol:   p.StockCode,
				})
			}
		}
		if p.HasQuote && p.UnrealizedPct <= -cfg.Rules.WarnUnrealizedLossPct {
			violations = append(violations, RiskViolation{
				RuleID:   "large_unrealized_loss",
				Severity: "warn",
				Message:  fmt.Sprintf("%s 浮亏 %.1f%% 超过警戒线", label(p), p.UnrealizedPct),
				Symbol:   p.StockCode,
				Value:    p.UnrealizedPct,
				Limit:    -cfg.Rules.WarnUnrealizedLossPct,
			})
		}
	}

	for _, exp := range exposures {
		if exp.WeightPct > cfg.Rules.MaxThemeWeightPct {
			violations = append(violations, RiskViolation{
				RuleID:   "max_theme_weight",
				Severity: "warn",
				Message:  fmt.Sprintf("主题「%s」暴露 %.1f%% 超过上限 %.0f%%", exp.ThemeName, exp.WeightPct, cfg.Rules.MaxThemeWeightPct),
				ThemeID:  exp.ThemeID,
				Value:    exp.WeightPct,
				Limit:    cfg.Rules.MaxThemeWeightPct,
			})
		}
		if _, ok := warnStates[strings.ToLower(exp.State)]; ok && exp.WeightPct >= 5 {
			violations = append(violations, RiskViolation{
				RuleID:   "theme_state_exposure",
				Severity: "warn",
				Message:  fmt.Sprintf("主题「%s」处于 %s，你暴露 %.1f%%", exp.ThemeName, exp.State, exp.WeightPct),
				ThemeID:  exp.ThemeID,
				Value:    exp.WeightPct,
			})
		}
	}

	return RiskReport{
		Passed:     len(violations) == 0,
		Violations: violations,
	}
}

func label(p portfolio.PositionView) string {
	name := strings.TrimSpace(p.StockName)
	if name == "" {
		name = p.StockCode
	}
	return name
}

func normalizeAsset(t string) string {
	if strings.EqualFold(t, portfolio.AssetFund) {
		return portfolio.AssetFund
	}
	return portfolio.AssetStock
}
