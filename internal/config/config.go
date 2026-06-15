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
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Addr      string `yaml:"addr"`
	AuthToken string `yaml:"auth_token"`
}

// ProviderDef defines a single provider endpoint.
type ProviderDef struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
}

// PresetConfig holds preset directory and inline items.
type PresetConfig struct {
	Dir   string                  `yaml:"dir"`
	Items map[string]PresetInline `yaml:"items,omitempty"`
}

// PresetInline is used for presets defined directly in config.yaml.
type PresetInline struct {
	Description string                `yaml:"description"`
	Panel       []types.PanelMember   `yaml:"panel"`
	Judge       types.JudgeConfig     `yaml:"judge"`
}

// FusionConfig holds fusion engine tuning parameters.
type FusionConfig struct {
	DefaultTimeout      int `yaml:"default_timeout"`
	MaxConcurrent       int `yaml:"max_concurrent"`
	PanelTimeoutPerModel int `yaml:"panel_timeout_per_model"`
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
			Items: make(map[string]PresetInline),
		},
		Fusion: FusionConfig{
			DefaultTimeout:       120,
			MaxConcurrent:        8,
			PanelTimeoutPerModel: 60,
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
