// Package fusion implements the core orchestration: API → panel → judge → response.
package fusion

import (
	"context"
	"fmt"
	"time"

	"github.com/lhy/openfusion/internal/api"
	"github.com/lhy/openfusion/internal/judge"
	"github.com/lhy/openfusion/internal/panel"
	"github.com/lhy/openfusion/internal/preset"
	"github.com/lhy/openfusion/internal/provider"
	"github.com/lhy/openfusion/internal/types"
)

// Engine implements api.FusionEngine.
type Engine struct {
	presetRegistry *preset.Registry
	panelDispatch  *panel.Dispatcher
	judgeSynth     *judge.Synthesizer
	defaultTimeout time.Duration
}

// NewEngine creates the fusion orchestration engine.
func NewEngine(
	pr *preset.Registry,
	pm *provider.Manager,
	panelTimeout time.Duration,
	judgeTimeout time.Duration,
	defaultTimeout time.Duration,
) *Engine {
	return &Engine{
		presetRegistry: pr,
		panelDispatch:  panel.NewDispatcher(pm, panelTimeout),
		judgeSynth:     judge.NewSynthesizer(pm, judgeTimeout),
		defaultTimeout: defaultTimeout,
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

// Execute runs the full fusion pipeline: panel → judge → response.
func (e *Engine) Execute(presetName string, req *types.ChatRequest) (*types.ChatResponse, error) {
	p, ok := e.presetRegistry.Get(presetName)
	if !ok {
		return nil, fmt.Errorf("unknown model: %s", presetName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), e.defaultTimeout)
	defer cancel()

	// Extract the user's primary prompt (last user message)
	prompt := extractLastUserMessage(req.Messages)
	if prompt == "" {
		return nil, fmt.Errorf("no user message found in request")
	}

	// Step 1: Dispatch panel in parallel
	panelResponses := e.panelDispatch.Dispatch(ctx, p, req)

	// Step 2: Judge synthesis
	judgeCfg := p.Judge
	result, err := e.judgeSynth.Synthesize(ctx, judgeCfg, prompt, panelResponses)
	if err != nil {
		return nil, fmt.Errorf("judge synthesis: %w", err)
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
