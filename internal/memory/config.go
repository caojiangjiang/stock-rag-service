package memory

import "time"

// Config holds TTL and capacity defaults for memory tiers.
type Config struct {
	ShortTTL      time.Duration
	ShortMaxMsgs  int
	MediumTTL     time.Duration
	ShortTermTTL  time.Duration // legacy manager
	MediumTermTTL time.Duration // legacy manager
	LongTermTTL   time.Duration // legacy manager
}

// DefaultConfig returns production-friendly defaults.
func DefaultConfig() Config {
	return Config{
		ShortTTL:      time.Hour,
		ShortMaxMsgs:  20,
		MediumTTL:     24 * time.Hour,
		ShortTermTTL:  time.Hour,
		MediumTermTTL: 24 * time.Hour,
		LongTermTTL:   7 * 24 * time.Hour,
	}
}
