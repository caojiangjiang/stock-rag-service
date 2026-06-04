package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"stock_rag/internal/persona/model"
)

type Config struct {
	Personas []model.PersonaProfile `yaml:"personas"`
}

type PersonaConfigLoader struct {
	config   *Config
	profiles map[string]*model.PersonaProfile
}

func NewPersonaConfigLoader(configPath string) (*PersonaConfigLoader, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read persona config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse persona config: %w", err)
	}

	profiles := make(map[string]*model.PersonaProfile)
	for i := range cfg.Personas {
		profiles[cfg.Personas[i].PersonaID] = &cfg.Personas[i]
	}

	return &PersonaConfigLoader{
		config:   &cfg,
		profiles: profiles,
	}, nil
}

func (l *PersonaConfigLoader) ListPersonas(market, styleTag, status string, limit int) ([]model.PersonaCard, error) {
	if limit <= 0 {
		limit = 20
	}

	cards := []model.PersonaCard{}
	for _, profile := range l.config.Personas {
		// 过滤 market
		if market != "" && profile.Market != market {
			continue
		}

		// 过滤 status
		if status != "" && profile.Status != status {
			continue
		}

		// 过滤 style_tag
		if styleTag != "" {
			found := false
			for _, tag := range profile.StyleTags {
				if tag == styleTag {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		card := model.PersonaCard{
			PersonaID:                profile.PersonaID,
			Name:                     profile.Name,
			Market:                   profile.Market,
			StyleTags:                profile.StyleTags,
			OneLiner:                 profile.OneLiner,
			RiskLevel:                getRiskLevel(profile.Performance.MaxDrawdown1Y),
			Performance:              profile.Performance,
			PreferredSectors:         profile.PreferredSectors,
			SuitableMarketConditions: profile.SuitableMarketConditions,
			AvatarKey:                profile.PersonaID,
			Status:                   profile.Status,
		}
		cards = append(cards, card)

		if len(cards) >= limit {
			break
		}
	}

	return cards, nil
}

func (l *PersonaConfigLoader) GetPersona(personaID string) (*model.PersonaProfile, error) {
	profile, ok := l.profiles[personaID]
	if !ok {
		return nil, fmt.Errorf("persona not found: %s", personaID)
	}
	return profile, nil
}

func (l *PersonaConfigLoader) GetAllPersonas() []model.PersonaProfile {
	return l.config.Personas
}

func getRiskLevel(maxDrawdown float64) string {
	switch {
	case maxDrawdown >= -10:
		return "low"
	case maxDrawdown >= -20:
		return "medium"
	default:
		return "high"
	}
}
