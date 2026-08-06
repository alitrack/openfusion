package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lhy/openfusion/internal/api"
	"github.com/lhy/openfusion/internal/cache"
	"github.com/lhy/openfusion/internal/config"
	"github.com/lhy/openfusion/internal/fusion"
	"github.com/lhy/openfusion/internal/health"
	"github.com/lhy/openfusion/internal/judge"
	"github.com/lhy/openfusion/internal/logger"
	"github.com/lhy/openfusion/internal/logging"
	"github.com/lhy/openfusion/internal/metrics"
	"github.com/lhy/openfusion/internal/openrouter"
	"github.com/lhy/openfusion/internal/panel"
	"github.com/lhy/openfusion/internal/plugin"
	"github.com/lhy/openfusion/internal/preset"
	"github.com/lhy/openfusion/internal/provider"
	"github.com/lhy/openfusion/internal/ratelimit"
	"github.com/lhy/openfusion/internal/skill"
	"github.com/lhy/openfusion/internal/tracing"
)

var log = logger.New(nil) // top-level logger

func main() {
	configPath := flag.String("config", "config.yaml", "path to config YAML file")
	flag.Parse()

	// Handle subcommands
	if flag.NArg() > 0 && flag.Arg(0) == "bench" {
		runBench(flag.Args()[1:])
		return
	}

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal("failed to load config", err, "path", *configPath)
	}

	// Configure log level from config
	setLogLevel(cfg.Log.Level)
	log.Info("config loaded", "path", *configPath, "log_level", cfg.Log.Level)

	// Initialize provider manager
	pm := provider.NewManager()

	// Register built-in plugins
	plugin.Register(&plugin.DeepSeekPlugin{})
	plugin.Register(&plugin.GenericPlugin{})
	plugin.Register(&openrouter.GatewayPlugin{})

	for name, def := range cfg.Providers {
		adapter := provider.NewOpenAIAdapter(name, def.BaseURL, def.APIKey)

		// Attach plugin if configured
		pluginName := def.Plugin
		if pluginName == "" {
			pluginName = "generic"
		}
		if p := plugin.Get(pluginName); p != nil {
			adapter.SetPlugin(p)
			log.Info("provider registered", "name", name, "url", def.BaseURL, "plugin", pluginName)
		} else {
			log.Warn("plugin not found, using generic", "provider", name, "plugin", pluginName)
			adapter.SetPlugin(plugin.Get("generic"))
		}

		pm.Register(name, adapter)
	}

	if len(cfg.Providers) == 0 {
		log.Warn("no providers configured — panel calls will fail")
	}

	// Initialize preset registry
	pr := preset.NewRegistry()

	// Load presets from directory
	if cfg.Presets.Dir != "" {
		if err := pr.LoadDir(cfg.Presets.Dir); err != nil {
			log.Warn("loading presets", "dir", cfg.Presets.Dir, "error", err.Error())
		}
	}

	// Load inline presets
	if len(cfg.Presets.Items) > 0 {
		if err := pr.LoadInline(cfg.Presets.Items); err != nil {
			log.Fatal("loading inline presets", err)
		}
	}

	presetList := pr.List()
	if len(presetList) == 0 {
		log.Fatal("no presets loaded — at least one preset is required", nil)
	}
	log.Info("presets loaded", "count", fmt.Sprintf("%d", len(presetList)))
	for _, p := range presetList {
		log.Debug("preset", "name", "openfusion/"+p.Name, "desc", p.Description)
	}

	// Create fusion engine
	panelTimeout := time.Duration(cfg.Fusion.PanelTimeoutPerModel) * time.Second
	judgeTimeout := time.Duration(cfg.Fusion.PanelTimeoutPerModel*2) * time.Second
	defaultTimeout := time.Duration(cfg.Fusion.DefaultTimeout) * time.Second

	mc := metrics.NewCollector()

	// Create response cache
	fusionCache, err := cache.New(cache.Config{
		Enabled: cfg.Cache.Enabled,
		MaxSize: cfg.Cache.MaxSize,
		TTL:     parseDuration(cfg.Cache.TTL, 300),
	})
	if err != nil {
		log.Fatal("failed to create cache", err)
	}

	// Create health checker
	healthConfigs := make(map[string]health.Config)
	for name, pd := range cfg.Providers {
		hcfg := health.Config{Enabled: false}
		if pd.HealthCheck != nil {
			hcfg.Enabled = pd.HealthCheck.Enabled
			hcfg.Interval = parseDuration(pd.HealthCheck.Interval, 30)
			hcfg.Timeout = parseDuration(pd.HealthCheck.Timeout, 10)
			hcfg.Endpoint = pd.HealthCheck.Endpoint
			hcfg.FailureThreshold = pd.HealthCheck.FailureThreshold
		}
		healthConfigs[name] = hcfg
	}
	healthChecker := health.NewChecker(healthConfigs)
	if cfgHasEnabledHealth(healthConfigs) {
		healthChecker.Start(context.Background())
		log.Info("health checks enabled")
	}

	// Create OTel tracer
	tracer := tracing.NewTracer()
	if tracer.Enabled() {
		log.Info("tracing enabled", "endpoint", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	} else {
		log.Debug("tracing disabled (set OTEL_EXPORTER_OTLP_ENDPOINT to enable)")
	}

	// Initialize skill system
	skillRegistry := skill.NewRegistry()
	skillDir := "skills"
	if _, err := os.Stat(skillDir); err == nil {
		if err := skillRegistry.LoadDir(skillDir); err != nil {
			log.Warn("loading skills", "dir", skillDir, "error", err.Error())
		}
	} else {
		log.Debug("no skills/ directory found, auto-generating from presets")
	}
	// Auto-generate skills from presets (backward compat)
	skillRegistry.LoadPresets(presetList)
	log.Info("skills loaded", "count", fmt.Sprintf("%d", len(skillRegistry.List())))

	// Create skill matcher and executor
	sm := skillRegistry.Matcher("openfusion/budget")
	se := skill.NewExecutor(pm,
		panel.NewDispatcher(pm, panelTimeout, healthChecker, 0, 0),
		judge.NewSynthesizer(pm, judgeTimeout),
		defaultTimeout)

	engine := fusion.NewEngine(pr, pm, panelTimeout, judgeTimeout, defaultTimeout, mc, fusionCache, healthChecker, tracer, sm, se, fusion.NewModelRouter(fusion.DefaultRouterConfig()))

	// DAG planner is configured via config.yaml dag.planner
	// and read internally by ExecuteDAG (no separate setter needed)

	// Setup SIGHUP hot-reload
	engine.SetConfigPath(*configPath)
	setupSignalHandler(engine, *configPath)

	// Create rate limiter
	presetNames := make([]string, len(presetList))
	for i, p := range presetList {
		presetNames[i] = p.Name
	}
	rl := ratelimit.NewLimiter(ratelimit.Config{
		Enabled: cfg.RateLimit.Enabled,
		Default: ratelimit.LimitConfig{
			Rate:  cfg.RateLimit.Default.Rate,
			Burst: cfg.RateLimit.Default.Burst,
		},
		Presets: func() map[string]ratelimit.LimitConfig {
			m := make(map[string]ratelimit.LimitConfig, len(cfg.RateLimit.Presets))
			for k, v := range cfg.RateLimit.Presets {
				m[k] = ratelimit.LimitConfig{Rate: v.Rate, Burst: v.Burst}
			}
			return m
		}(),
	}, presetNames)

	// Create fusion logging hook
	fusionHook := logging.NewHook(cfg.Logging.Hook)
	if cfg.Logging.Hook.Enabled {
		log.Info("fusion logging hook enabled", "dir", cfg.Logging.Hook.OutputDir, "split", cfg.Logging.Hook.AutoSplit)
	}

	// Create API server
	apiServer := api.NewServer(engine, cfg.Server.AuthToken, cfg.Server.Addr, rl, fusionHook)

	// HTTP server
	httpServer := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // 0 for SSE streaming
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("shutdown panic", fmt.Errorf("%v", r))
			}
		}()
		<-sigCh
		log.Info("shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Error("shutdown error", err)
		}
		fusionHook.Close()
		log.Info("fusion logging hook closed")
	}()

	log.Info("server starting", "addr", cfg.Server.Addr, "presets", fmt.Sprintf("%d", len(presetList)), "skills", fmt.Sprintf("%d", len(skillRegistry.List())))

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("server error", err)
	}

	log.Info("server stopped")
}

// parseDuration parses a duration string like "30s" or "5m".
// Returns defaultVal (in seconds) on empty or parse error.
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

// cfgHasEnabledHealth returns true if any provider has health check enabled.
func cfgHasEnabledHealth(configs map[string]health.Config) bool {
	for _, c := range configs {
		if c.Enabled {
			return true
		}
	}
	return false
}

// setLogLevel configures the log level from config string.
func setLogLevel(level string) {
	switch strings.ToLower(level) {
	case "debug":
		log.SetLevel(logger.LevelDebug)
	case "warn", "warning":
		log.SetLevel(logger.LevelWarn)
	case "error":
		log.SetLevel(logger.LevelError)
	default:
		log.SetLevel(logger.LevelInfo)
	}
}

// setupSignalHandler listens for SIGHUP to trigger config hot-reload.
func setupSignalHandler(engine *fusion.Engine, cfgPath string) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("signal handler panic", fmt.Errorf("%v", r))
			}
		}()
		for range sigCh {
			log.Info("SIGHUP received, reloading config", "path", cfgPath)
			if err := engine.Reload(cfgPath); err != nil {
				log.Error("config reload failed", err, "path", cfgPath)
			} else {
				log.Info("config reloaded successfully")
			}
		}
	}()
}
