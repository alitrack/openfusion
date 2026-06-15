// Package fusion implements the core orchestration: API → panel → judge → response.
package fusion

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lhy/openfusion/internal/api"
	"github.com/lhy/openfusion/internal/cache"
	"github.com/lhy/openfusion/internal/codex"
	"github.com/lhy/openfusion/internal/config"
	"github.com/lhy/openfusion/internal/judge"
	"github.com/lhy/openfusion/internal/metrics"
	"github.com/lhy/openfusion/internal/panel"
	"github.com/lhy/openfusion/internal/preset"
	"github.com/lhy/openfusion/internal/provider"
	"github.com/lhy/openfusion/internal/search"
	"github.com/lhy/openfusion/internal/skill"
	"github.com/lhy/openfusion/internal/tracing"
	"github.com/lhy/openfusion/internal/types"
)

// Engine implements api.FusionEngine.
type Engine struct {
	mu             sync.RWMutex
	presetRegistry *preset.Registry
	panelDispatch  *panel.Dispatcher
	judgeSynth     *judge.Synthesizer
	defaultTimeout time.Duration
	metrics        *metrics.Collector
	cache          *cache.Cache
	tracer         *tracing.Tracer
	skillMatcher   *skill.Matcher
	skillExecutor  *skill.Executor
	providerMgr    *provider.Manager
	configPath     string
}

// NewEngine creates the fusion orchestration engine.
func NewEngine(
	pr *preset.Registry,
	pm *provider.Manager,
	panelTimeout time.Duration,
	judgeTimeout time.Duration,
	defaultTimeout time.Duration,
	mc *metrics.Collector,
	ca *cache.Cache,
	hc panel.HealthChecker,
	tr *tracing.Tracer,
	sm *skill.Matcher,
	se *skill.Executor,
) *Engine {
	return &Engine{
		presetRegistry: pr,
		panelDispatch:  panel.NewDispatcher(pm, panelTimeout, hc, 0, 0),
		judgeSynth:     judge.NewSynthesizer(pm, judgeTimeout),
		defaultTimeout: defaultTimeout,
		metrics:        mc,
		cache:          ca,
		tracer:         tr,
		skillMatcher:   sm,
		skillExecutor:  se,
		providerMgr:    pm,
	}
}

// Reload re-reads the config file and hot-swaps engine internals.
func (e *Engine) Reload(cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("reload config: %w", err)
	}

	newEngine, _, err := buildFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("rebuild engine: %w", err)
	}

	// Atomic swap
	e.mu.Lock()
	e.presetRegistry = newEngine.presetRegistry
	e.panelDispatch = newEngine.panelDispatch
	e.judgeSynth = newEngine.judgeSynth
	e.defaultTimeout = newEngine.defaultTimeout
	e.cache = newEngine.cache
	e.skillMatcher = newEngine.skillMatcher
	e.skillExecutor = newEngine.skillExecutor
	e.providerMgr = newEngine.providerMgr
	e.mu.Unlock()

	return nil
}

// getProviderManager returns the current provider manager.
func (e *Engine) getProviderManager() *provider.Manager {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.providerMgr
}

// SetConfigPath stores the config path for use by Reload.
func (e *Engine) SetConfigPath(path string) {
	e.configPath = path
}

// ListPresets returns all registered presets as API summaries.
func (e *Engine) ListPresets() []api.PresetSummary {
	presets := e.presetRegistry.List()
	summaries := make([]api.PresetSummary, 0, len(presets))
	for _, p := range presets {
		summaries = append(summaries, api.PresetSummary{
			ID:          "openfusion/" + p.Name,
			Object:      "model",
			Created:     time.Now().Unix(),
			OwnedBy:     "openfusion",
			Description: p.Description,
		})
	}
	return summaries
}

// Metrics returns the metrics collector snapshot.
func (e *Engine) Metrics() interface{} {
	if e.metrics == nil {
		return nil
	}
	return e.metrics.Snapshot()
}

// CreatePreset adds a new preset at runtime.
func (e *Engine) CreatePreset(name string, preset types.Preset) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	p := preset
	p.Name = name
	return e.presetRegistry.Register(&p)
}

// DeletePreset removes a preset by name.
func (e *Engine) DeletePreset(name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.presetRegistry.Remove(name)
}

// GetPreset returns a preset by name.
func (e *Engine) GetPreset(name string) (*types.Preset, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	p, ok := e.presetRegistry.Get(name)
	if !ok {
		return nil, fmt.Errorf("preset not found: %s", name)
	}
	return p, nil
}

// ExecuteAuto uses skill matching to automatically route a request.
// Falls back to the default preset if no skill matches.
func (e *Engine) ExecuteAuto(req *types.ChatRequest) (*types.ChatResponse, error) {
	// No skill system initialized — fall back to preset
	if e.skillMatcher == nil || e.skillExecutor == nil {
		presets := e.presetRegistry.List()
		if len(presets) > 0 {
			return e.Execute(presets[0].Name, req)
		}
		return nil, fmt.Errorf("skill system not initialized and no fallback preset available")
	}

	// Analyze request features
	features := skill.AnalyzeRequest(req)

	// Match skill
	matched := e.skillMatcher.Match(features)
	if matched == nil {
		// No skill matched — fall back to default fusion
		fallback := e.skillMatcher.DefaultRef()
		if fallback == "" || fallback == "direct" {
			// Use first available preset as fallback
			presets := e.presetRegistry.List()
			if len(presets) > 0 {
				return e.Execute(presets[0].Name, req)
			}
			return nil, fmt.Errorf("no skills matched and no fallback preset available")
		}
		return e.Execute(fallback, req)
	}

	if e.metrics != nil {
		e.metrics.RecordRequest(matched.Name)
	}

	// Execute skill with timeout
	ctx, cancel := context.WithTimeout(context.Background(), e.defaultTimeout)
	defer cancel()
	resp, err := e.skillExecutor.Execute(ctx, matched, req)
	if err != nil {
		return nil, fmt.Errorf("skill '%s' execution: %w", matched.Name, err)
	}

	return resp, nil
}

// Execute runs the full fusion pipeline: panel → judge → response.
func (e *Engine) Execute(presetName string, req *types.ChatRequest) (*types.ChatResponse, error) {
	p, ok := e.presetRegistry.Get(presetName)
	if !ok {
		return nil, fmt.Errorf("unknown model: %s", presetName)
	}

	// Apply request-level overrides (panel/judge) before execution
	p = applyPresetOverrides(p, req.PanelOverride, req.JudgeOverride)

	ctx := context.Background()

	// Start root tracing span
	var rootSpan tracing.Span
	if e.tracer != nil && e.tracer.Enabled() {
		ctx, rootSpan = e.tracer.StartSpan(ctx, "Fusion.Execute",
			tracing.AttrPreset.String(presetName),
			tracing.AttrPanelCount.Int(len(p.Panel)),
			tracing.AttrJudgeModel.String(p.Judge.Model),
		)
		defer rootSpan.End()
	}

	if e.metrics != nil {
		e.metrics.RecordRequest(presetName)
	}
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, e.defaultTimeout)
	defer cancel()

	// Extract the user's primary prompt (last user message)
	prompt := types.ExtractLastUserMessage(req.Messages)
	if prompt == "" {
		if e.metrics != nil {
			e.metrics.RecordFusionComplete(presetName, time.Since(start), false)
		}
		return nil, fmt.Errorf("no user message found in request")
	}

	// Step 0: Cache check before any API calls
	cacheKey := cache.Key(presetName, req.Messages, req.PanelOverride, req.JudgeOverride)
	if e.cache != nil && e.cache.Enabled() {
		if cached := e.cache.Get(cacheKey); cached != nil {
			return cached, nil
		}
	}

	// Step 0.5: Web search context injection
	searchMessages := search.AddSearchContext(req.Messages, p.WebSearch)
	searchReq := *req
	searchReq.Messages = searchMessages
	req = &searchReq

	// Step 1: Dispatch panel in parallel
	var panelSpan tracing.Span
	if e.tracer != nil && e.tracer.Enabled() {
		ctx, panelSpan = e.tracer.StartSpan(ctx, "panel.dispatch",
			tracing.AttrPreset.String(presetName),
			tracing.AttrPanelCount.Int(len(p.Panel)),
		)
	}
	panelResponses := e.panelDispatch.Dispatch(ctx, p, req)
	if panelSpan != nil {
		panelSpan.End()
	}

	// Record panel metrics
	if e.metrics != nil {
		for _, pr := range panelResponses {
			success := len(pr.Error) == 0 && !pr.TimedOut
			e.metrics.RecordPanelCall(presetName, pr.Member.Model, pr.Duration, pr.Usage.TotalTokens, pr.Usage.CostUSD, success)
		}
	}

	// Step 1a: If judge=false, return panel responses directly
	if req.NoJudge != nil && *req.NoJudge {
		return buildPanelOnlyResponse(presetName, panelResponses), nil
	}

	// Step 2: Judge synthesis
	judgeCfg := p.Judge
	var judgeSpan tracing.Span
	if e.tracer != nil && e.tracer.Enabled() {
		ctx, judgeSpan = e.tracer.StartSpan(ctx, "judge.synthesize",
			tracing.AttrJudgeModel.String(judgeCfg.Model),
		)
	}
	result, err := e.judgeSynth.Synthesize(ctx, judgeCfg, prompt, panelResponses)
	if judgeSpan != nil {
		if err != nil {
			judgeSpan.RecordError(err)
			judgeSpan.SetAttributes(tracing.AttrSuccess.Bool(false))
		} else {
			judgeSpan.SetAttributes(
				tracing.AttrDuration.Int64(time.Since(start).Milliseconds()),
				tracing.AttrTokenCount.Int(result.Usage.TotalTokens),
				tracing.AttrCostUSD.Float64(result.Usage.CostUSD),
				tracing.AttrSuccess.Bool(true),
			)
		}
		judgeSpan.End()
	}
	if err != nil {
		if e.metrics != nil {
			e.metrics.RecordFusionComplete(presetName, time.Since(start), false)
		}
		return nil, fmt.Errorf("judge synthesis: %w", err)
	}

	if e.metrics != nil {
		e.metrics.RecordJudgeCall(presetName, time.Since(start), result.Usage.TotalTokens, result.Usage.CostUSD)
		e.metrics.RecordFusionComplete(presetName, time.Since(start), true)
	}

	// Step 3: Format as OpenAI-compatible response
	resp := &types.ChatResponse{
		ID:      fmt.Sprintf("ofusion_%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Model:   presetName,
		Choices: []types.Choice{
			{Index: 0, Message: types.ChatMessage{Role: "assistant", Content: result.Answer}},
		},
		Usage:    result.Usage,
		Analysis: result.Analysis,
	}

	// Attach structured codex output if requested
	if req.Codex {
		panelCount := len(panelResponses)
		cx := codex.Extract(result.Answer, panelCount)
		resp.Codex = cx
	}

	// Step 3b: Cache the result
	if e.cache != nil && e.cache.Enabled() {
		e.cache.Set(cacheKey, resp)
	}

	return resp, nil
}

// buildPanelOnlyResponse constructs a response without judge synthesis.
func buildPanelOnlyResponse(presetName string, responses []types.PanelResponse) *types.ChatResponse {
	var b strings.Builder
	summaries := make([]types.PanelResponseSummary, 0, len(responses))
	totalUsage := types.Usage{}

	for _, pr := range responses {
		summary := types.PanelResponseSummary{
			Model:      pr.Member.Model,
			DurationMs: pr.Duration.Milliseconds(),
			CostUSD:    pr.Usage.CostUSD,
			Error:      pr.Error,
		}
		if pr.TimedOut {
			summary.Error = "timeout"
		}
		if pr.Error == "" && !pr.TimedOut {
			summary.Content = pr.Content
			summary.PromptTokens = pr.Usage.PromptTokens
			summary.CompletionTokens = pr.Usage.CompletionTokens
			summary.TotalTokens = pr.Usage.TotalTokens
			totalUsage.PromptTokens += pr.Usage.PromptTokens
			totalUsage.CompletionTokens += pr.Usage.CompletionTokens
			totalUsage.TotalTokens += pr.Usage.TotalTokens
			totalUsage.CostUSD += pr.Usage.CostUSD
		}

		// Build concatenated text
		b.WriteString("=== ")
		b.WriteString(pr.Member.Model)
		b.WriteString(" ===\n")
		if pr.Error != "" {
			b.WriteString("[ERROR: ")
			b.WriteString(pr.Error)
			b.WriteString("]\n")
		} else {
			b.WriteString(pr.Content)
		}
		b.WriteString("\n\n")

		summaries = append(summaries, summary)
	}

	return &types.ChatResponse{
		ID:      fmt.Sprintf("ofusion_%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Model:   presetName,
		Choices: []types.Choice{
			{Index: 0, Message: types.ChatMessage{Role: "assistant", Content: strings.TrimSpace(b.String())}},
		},
		Usage:          totalUsage,
		PanelResponses: summaries,
	}
}

// applyPresetOverrides applies request-level panel/judge overrides to a preset.
// Returns a new copy when overrides are present; returns the original pointer
// when both are nil (zero allocation for common case).
func applyPresetOverrides(p *types.Preset, panelOverride []types.PanelMember, judgeOverride *types.JudgeConfig) *types.Preset {
	if panelOverride == nil && judgeOverride == nil {
		return p
	}
	cp := deepCopyPreset(p)
	if panelOverride != nil {
		cp.Panel = make([]types.PanelMember, len(panelOverride))
		copy(cp.Panel, panelOverride)
	}
	if judgeOverride != nil {
		cp.Judge = *judgeOverride
	}
	return cp
}

// deepCopyPreset creates an independent deep copy of a preset.
func deepCopyPreset(p *types.Preset) *types.Preset {
	cp := &types.Preset{
		Name:        p.Name,
		Description: p.Description,
		Judge:       p.Judge,
	}
	if len(p.Panel) > 0 {
		cp.Panel = make([]types.PanelMember, len(p.Panel))
		copy(cp.Panel, p.Panel)
	}
	if p.WebSearch != nil {
		w := *p.WebSearch
		cp.WebSearch = &w
	}
	return cp
}
