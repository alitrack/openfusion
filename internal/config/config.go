// Package config handles YAML configuration loading with environment variable substitution.
package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"

	"github.com/lhy/openfusion/internal/types"
)

// Config is the root configuration structure.
type Config struct {
	Server    ServerConfig            `yaml:"server"`
	Providers map[string]ProviderDef  `yaml:"providers"`
	Presets   PresetConfig            `yaml:"presets"`
	Fusion    FusionConfig            `yaml:"fusion"`
	RateLimit RateLimitConfig         `yaml:"rate_limit"`
	Cache     CacheConfig             `yaml:"cache"`
}

// CacheConfig holds response cache configuration.
type CacheConfig struct {
	Enabled  bool              `yaml:"enabled"`
	MaxSize  int               `yaml:"max_size"`
	TTL      string            `yaml:"ttl"` // e.g. "300s"
	Presets  map[string]string `yaml:"presets,omitempty"` // per-preset TTL, e.g. "budget": "600s"
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Addr      string `yaml:"addr"`
	AuthToken string `yaml:"auth_token"`
}

// ProviderDef defines a single provider endpoint.
type ProviderDef struct {
	BaseURL     string            `yaml:"base_url"`
	APIKey      string            `yaml:"api_key"`
	HealthCheck *HealthCheckDef   `yaml:"health_check,omitempty"`
}

// HealthCheckDef defines health check parameters for a provider.
type HealthCheckDef struct {
	Enabled          bool   `yaml:"enabled"`
	Interval         string `yaml:"interval,omitempty"`   // e.g. "30s"
	Timeout          string `yaml:"timeout,omitempty"`    // e.g. "10s"
	Endpoint         string `yaml:"endpoint,omitempty"`
	FailureThreshold int    `yaml:"failure_threshold,omitempty"`
}

// PresetConfig holds preset directory and inline items.
type PresetConfig struct {
	Dir   string                     `yaml:"dir"`
	Items map[string]types.InlinePreset `yaml:"items,omitempty"`
}

// FusionConfig holds fusion engine tuning parameters.
type FusionConfig struct {
	DefaultTimeout       int `yaml:"default_timeout"`
	MaxConcurrent        int `yaml:"max_concurrent"`
	PanelTimeoutPerModel int `yaml:"panel_timeout_per_model"`
}

// RateLimitConfig holds rate limiting configuration.
type RateLimitConfig struct {
	Enabled bool                      `yaml:"enabled"`
	Default RateLimitPreset           `yaml:"default"`
	Presets map[string]RateLimitPreset `yaml:"presets,omitempty"`
}

// RateLimitPreset defines rate and burst for one or all presets.
type RateLimitPreset struct {
	Rate  float64 `yaml:"rate"`
	Burst int     `yaml:"burst"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Addr: "127.0.0.1:8080",
		},
		Providers: make(map[string]ProviderDef),
		Presets: PresetConfig{
			Dir:   "presets",
			Items: make(map[string]types.InlinePreset),
		},
		Fusion: FusionConfig{
			DefaultTimeout:       120,
			MaxConcurrent:        8,
			PanelTimeoutPerModel: 60,
		},
		RateLimit: RateLimitConfig{
			Enabled: false,
			Default: RateLimitPreset{Rate: 10, Burst: 20},
			Presets: make(map[string]RateLimitPreset),
		},
		Cache: CacheConfig{
			Enabled: false,
			MaxSize: 1000,
			TTL:     "300s",
			Presets: make(map[string]string),
		},
	}
}

// Load reads a YAML config file, substitutes ${ENV_VAR} references, and parses it.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	resolved := expandEnv(string(data))

	cfg := DefaultConfig()
	if err := yaml.Unmarshal([]byte(resolved), cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return cfg, nil
}

// envVarPattern matches ${VARIABLE_NAME} or ${VARIABLE_NAME:default_value}
var envVarPattern = regexp.MustCompile(`\$\{([^}:]+)(?::([^}]*))?\}`)

// expandEnv replaces ${VAR} and ${VAR:default} patterns with environment variables.
func expandEnv(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := envVarPattern.FindStringSubmatch(match)
		name := parts[1]
		defaultVal := parts[2]

		if val, ok := os.LookupEnv(name); ok {
			return val
		}
		if defaultVal != "" {
			return defaultVal
		}
		// Leave unresolved to avoid silently using empty string
		return match
	})
}
