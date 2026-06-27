// Package fusion implements the core orchestration: API → panel → judge → response.
package fusion

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/lhy/openfusion/internal/cache"
	"github.com/lhy/openfusion/internal/config"
	"github.com/lhy/openfusion/internal/health"
	"github.com/lhy/openfusion/internal/judge"
	"github.com/lhy/openfusion/internal/logger"
	"github.com/lhy/openfusion/internal/metrics"
	"github.com/lhy/openfusion/internal/panel"
	"github.com/lhy/openfusion/internal/preset"
	"github.com/lhy/openfusion/internal/provider"
	"github.com/lhy/openfusion/internal/skill"
	"github.com/lhy/openfusion/internal/tracing"
)

// BuildFromConfig creates a fully initialized Engine from a config file path.
// Returns the engine, a cleanup function, and any error.
// The cleanup function stops background goroutines (health checks, etc.).
func BuildFromConfig(cfgPath string) (*Engine, func(), error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}

	return buildFromConfig(cfg)
}

// buildFromConfig is the internal implementation shared by BuildFromConfig and Reload.
func buildFromConfig(cfg *config.Config) (*Engine, func(), error) {
	// Configure log level
	setLogLevel(cfg.Log.Level)
	logger.Info("building fusion engine", "log_level", cfg.Log.Level, "cache_enabled", fmt.Sprintf("%v", cfg.Cache.Enabled))

	// Initialize provider manager
	pm := provider.NewManager()

	// Register providers from config
	for name, pDef := range cfg.Providers {
		adapter := provider.NewOpenAIAdapter(name, pDef.BaseURL, pDef.APIKey)
		pm.Register(name, adapter)
		logger.Info("provider registered", "name", name, "base_url", pDef.BaseURL)
	}

	// Create preset registry
	pr := preset.NewRegistry()
	if err := pr.LoadDir(cfg.Presets.Dir); err != nil {
		return nil, nil, fmt.Errorf("load presets: %w", err)
	}
	if err := pr.LoadInline(cfg.Presets.Items); err != nil {
		return nil, nil, fmt.Errorf("load inline presets: %w", err)
	}
	presetList := pr.List()
	logger.Info("presets loaded", "count", fmt.Sprintf("%d", len(presetList)))

	// Create skill registry and load skill files
	skillRegistry := skill.NewRegistry()
	skillDir := skill.ResolveSkillDir(cfg.Presets.Dir)
	if skillDir != "" {
		if err := skillRegistry.LoadDir(skillDir); err != nil {
			return nil, nil, fmt.Errorf("load skills: %w", err)
		}
	}
	// Auto-generate skills from presets (backward compat)
	skillRegistry.LoadPresets(presetList)
	logger.Info("skills loaded", "count", fmt.Sprintf("%d", len(skillRegistry.List())))

	// Parse timeouts
	panelTimeout := time.Duration(cfg.Fusion.PanelTimeoutPerModel) * time.Second
	if panelTimeout <= 0 {
		panelTimeout = 60 * time.Second
	}
	judgeTimeout := time.Duration(cfg.Fusion.DefaultTimeout) * time.Second
	if judgeTimeout <= 0 {
		judgeTimeout = 120 * time.Second
	}
	defaultTimeout := time.Duration(cfg.Fusion.DefaultTimeout) * time.Second
	if defaultTimeout <= 0 {
		defaultTimeout = 120 * time.Second
	}

	// Create health checker
	healthChecker := health.NewChecker(make(map[string]health.Config))

	// Create skill matcher and executor
	sm := skillRegistry.Matcher("openfusion/budget")
	se := skill.NewExecutor(pm,
		panel.NewDispatcher(pm, panelTimeout, healthChecker, cfg.Fusion.MaxConcurrent, 0),
		judge.NewSynthesizer(pm, judgeTimeout),
		defaultTimeout)

	// Create metrics collector
	mc := metrics.NewCollector()

	// Create response cache
	fusionCache, err := cache.New(cache.Config{
		Enabled: cfg.Cache.Enabled,
		MaxSize: cfg.Cache.MaxSize,
		TTL:     parseDuration(cfg.Cache.TTL, 300),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create cache: %w", err)
	}

	// Create tracer
	tracer := tracing.NewTracer()

	// Load topology presets from directory
	topologyDir := cfg.Presets.Dir + "/topology"
	topologyPresets, err := loadTopologyPresets(topologyDir)
	if err != nil {
		logger.Warn("topology preset loading skipped", "error", err.Error())
	}
	if len(topologyPresets) > 0 {
		logger.Info("topology presets loaded", "count", fmt.Sprintf("%d", len(topologyPresets)))
	}

	// Create engine
	engine := NewEngine(pr, pm, panelTimeout, judgeTimeout, defaultTimeout, mc, fusionCache, healthChecker, tracer, sm, se)
	engine.topologyPresets = topologyPresets

	cleanup := func() {}

	return engine, cleanup, nil
}

// parseDuration parses a duration string like "300s" with a default in seconds.
func parseDuration(s string, defaultSeconds int) time.Duration {
	if s == "" {
		return time.Duration(defaultSeconds) * time.Second
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return time.Duration(defaultSeconds) * time.Second
	}
	return d
}

// setLogLevel configures the global log level from a string.
func setLogLevel(level string) {
	switch level {
	case "debug":
		logger.LogLevel = "debug"
	case "warn":
		logger.LogLevel = "warn"
	case "error":
		logger.LogLevel = "error"
	default:
		logger.LogLevel = "info"
	}
}

// loadTopologyPresets loads multi-layer topology definitions from a directory.
func loadTopologyPresets(dir string) (map[string]*TopologyDef, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // directory is optional
		}
		return nil, fmt.Errorf("read topology dir %s: %w", dir, err)
	}

	presets := make(map[string]*TopologyDef)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Name(), err)
		}

		var topo TopologyDef
		if err := yaml.Unmarshal(data, &topo); err != nil {
			return nil, fmt.Errorf("parse %s: %w", entry.Name(), err)
		}
		if err := topo.Validate(); err != nil {
			return nil, fmt.Errorf("validate %s: %w", entry.Name(), err)
		}

		// Use filename without extension as preset name
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		presets[name] = &topo
	}

	return presets, nil
}
