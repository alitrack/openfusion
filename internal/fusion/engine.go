// Package fusion implements the core orchestration: API → panel → judge → response.
package fusion

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lhy/openfusion/internal/api"
	"github.com/lhy/openfusion/internal/cache"
	"github.com/lhy/openfusion/internal/judge"
	"github.com/lhy/openfusion/internal/metrics"
	"github.com/lhy/openfusion/internal/panel"
	"github.com/lhy/openfusion/internal/preset"
	"github.com/lhy/openfusion/internal/provider"
	"github.com/lhy/openfusion/internal/tracing"
	"github.com/lhy/openfusion/internal/types"
)

// Engine implements api.FusionEngine.
type Engine struct {
	presetRegistry *preset.Registry
	panelDispatch  *panel.Dispatcher
	judgeSynth     *judge.Synthesizer
	defaultTimeout time.Duration
	metrics        *metrics.Collector
	cache          *cache.Cache
	tracer         *tracing.Tracer
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
) *Engine {
	return &Engine{
		presetRegistry: pr,
		panelDispatch:  panel.NewDispatcher(pm, panelTimeout, hc),
		judgeSynth:     judge.NewSynthesizer(pm, judgeTimeout),
		defaultTimeout: defaultTimeout,
		metrics:        mc,
		cache:          ca,
		tracer:         tr,
	}
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

// Execute runs the full fusion pipeline: panel → judge → response.
func (e *Engine) Execute(presetName string, req *types.ChatRequest) (*types.ChatResponse, error) {
	p, ok := e.presetRegistry.Get(presetName)
	if !ok {
		return nil, fmt.Errorf("unknown model: %s", presetName)
	}

	ctx := context.Background()

	// Start root tracing span
	if e.tracer != nil && e.tracer.Enabled() {
		ctx, _ = e.tracer.StartSpan(ctx, "Fusion.Execute",
			tracing.AttrPreset.String(presetName),
			tracing.AttrPanelCount.Int(len(p.Panel)),
			tracing.AttrJudgeModel.String(p.Judge.Model),
		)
	}

	if e.metrics != nil {
		e.metrics.RecordRequest(presetName)
	}
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, e.defaultTimeout)
	defer cancel()

	// Extract the user's primary prompt (last user message)
	prompt := extractLastUserMessage(req.Messages)
	if prompt == "" {
		if e.metrics != nil {
			e.metrics.RecordFusionComplete(presetName, time.Since(start), false)
		}
		return nil, fmt.Errorf("no user message found in request")
	}

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

	// Step 1a: Cache check — generate key and check before judge
	cacheKey := cache.Key(presetName, req.Messages)
	if e.cache != nil && e.cache.Enabled() {
		if cached := e.cache.Get(cacheKey); cached != nil {
			return cached, nil
		}
	}

	// Step 1b: If judge=false, return panel responses directly
	if req.Judge != nil && !*req.Judge {
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

	// Step 3b: Cache the result
	if e.cache != nil && e.cache.Enabled() {
		e.cache.Set(cacheKey, resp)
	}

	return resp, nil
}

// extractLastUserMessage gets the content of the last user message.
func extractLastUserMessage(messages []types.ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
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
