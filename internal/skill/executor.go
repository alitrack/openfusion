package skill

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lhy/openfusion/internal/codex"
	"github.com/lhy/openfusion/internal/judge"
	"github.com/lhy/openfusion/internal/panel"
	"github.com/lhy/openfusion/internal/provider"
	"github.com/lhy/openfusion/internal/types"
)

// ---------------------------------------------------------------------------
// Executor
// ---------------------------------------------------------------------------

// Executor runs skills: translates a Skill definition into actual model calls.
type Executor struct {
	providerManager *provider.Manager
	panelDispatcher *panel.Dispatcher
	judgeSynth      *judge.Synthesizer
	defaultTimeout  time.Duration
}

// NewExecutor creates a skill executor.
func NewExecutor(pm *provider.Manager, pd *panel.Dispatcher, js *judge.Synthesizer, timeout time.Duration) *Executor {
	return &Executor{
		providerManager: pm,
		panelDispatcher: pd,
		judgeSynth:      js,
		defaultTimeout:  timeout,
	}
}

// Execute runs a skill against a request and returns the response.
func (e *Executor) Execute(ctx context.Context, s *Skill, req *types.ChatRequest) (*types.ChatResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, e.defaultTimeout)
	defer cancel()

	// Fill in missing panel member provider/model from strategy defaults
	s = inheritPanelDefaults(s)

	switch s.Mode {
	case ModeDirect:
		return e.executeDirect(ctx, s, req)
	case ModeSelfEnsemble:
		return e.executeSelfEnsemble(ctx, s, req)
	case ModeFusion:
		return e.executeFusion(ctx, s, req)
	default:
		return nil, fmt.Errorf("unknown skill mode: %s", s.Mode)
	}
}

// inheritPanelDefaults fills empty provider/model in panel members from strategy level.
func inheritPanelDefaults(s *Skill) *Skill {
	cp := *s
	cp.Strategy.Panel = make([]PanelMemberConfig, len(s.Strategy.Panel))
	for i, pm := range s.Strategy.Panel {
		cp.Strategy.Panel[i] = pm
		if cp.Strategy.Panel[i].Provider == "" {
			cp.Strategy.Panel[i].Provider = s.Strategy.Provider
		}
		if cp.Strategy.Panel[i].Model == "" {
			cp.Strategy.Panel[i].Model = s.Strategy.Model
		}
	}
	// Also default judge provider/model from strategy
	if cp.Strategy.Judge.Provider == "" {
		cp.Strategy.Judge.Provider = cp.Strategy.Provider
	}
	if cp.Strategy.Judge.Model == "" {
		cp.Strategy.Judge.Model = cp.Strategy.Model
	}
	return &cp
}

// ---------------------------------------------------------------------------
// Direct mode
// ---------------------------------------------------------------------------

func (e *Executor) executeDirect(ctx context.Context, s *Skill, req *types.ChatRequest) (*types.ChatResponse, error) {
	p, err := e.providerManager.Get(s.Strategy.Provider)
	if err != nil {
		return nil, fmt.Errorf("direct: provider %s: %w", s.Strategy.Provider, err)
	}

	// Build the request for this panel member — always copy messages
	panelReq := buildPanelRequest(s, req, s.Strategy.Provider, s.Strategy.Model, s.Strategy.System)

	resp, err := p.ChatCompletion(ctx, panelReq)
	if err != nil {
		return nil, fmt.Errorf("direct: chat completion: %w", err)
	}

	return resp, nil
}

// ---------------------------------------------------------------------------
// Self-ensemble mode
// ---------------------------------------------------------------------------

func (e *Executor) executeSelfEnsemble(ctx context.Context, s *Skill, req *types.ChatRequest) (*types.ChatResponse, error) {
	preset := &types.Preset{
		Name:        s.Name,
		Description: s.Description,
		Judge:       toJudgeConfig(s.Strategy.Judge),
	}

	for _, pm := range s.Strategy.Panel {
		base := pm.ToBase()
		preset.Panel = append(preset.Panel, base)
	}

	// Dispatch panel
	panelResponses := e.panelDispatcher.Dispatch(ctx, preset, req)

	// If judge is disabled, return raw panel responses
	if !s.Strategy.Judge.Enabled {
		return buildPanelOnlyResponse(s.Name, panelResponses), nil
	}

	// Extract user prompt
	prompt := types.ExtractLastUserMessage(req.Messages)
	if prompt == "" && len(req.Messages) > 0 {
		prompt = req.Messages[len(req.Messages)-1].Content
	}

	// Run judge synthesis
	judgeCfg := toJudgeConfig(s.Strategy.Judge)
	result, err := e.judgeSynth.Synthesize(ctx, judgeCfg, prompt, panelResponses)
	if err != nil {
		return nil, fmt.Errorf("self-ensemble judge: %w", err)
	}

	resp := &types.ChatResponse{
		ID:      fmt.Sprintf("ofusion_skill_%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Model:   "openfusion/" + s.Name,
		Choices: []types.Choice{{Index: 0, Message: types.ChatMessage{Role: "assistant", Content: result.Answer}}},
		Usage:   result.Usage,
		Analysis: result.Analysis,
	}

	// If codex mode is requested, extract structured code output
	if req.Codex {
		cx := codex.Extract(result.Answer, len(panelResponses))
		resp.Codex = cx
	}

	return resp, nil
}

// ---------------------------------------------------------------------------
// Fusion mode
// ---------------------------------------------------------------------------

func (e *Executor) executeFusion(ctx context.Context, s *Skill, req *types.ChatRequest) (*types.ChatResponse, error) {
	return e.executeSelfEnsemble(ctx, s, req)
}

// NOTE: ModeFusion and ModeSelfEnsemble currently behave identically.
// Future enhancement: use different judge instructions for cross-model fusion vs self-ensemble.

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildPanelRequest creates a single-model ChatRequest for direct mode.
func buildPanelRequest(s *Skill, req *types.ChatRequest, provider, model, system string) *types.ChatRequest {
	panelReq := &types.ChatRequest{
		Model:       model,
		MaxTokens:   s.Params.MaxTokens,
		Temperature: s.Params.Temperature,
	}

	if panelReq.MaxTokens == 0 {
		panelReq.MaxTokens = req.MaxTokens
	}
	if panelReq.Temperature == nil {
		panelReq.Temperature = req.Temperature
	}
	if panelReq.Temperature == nil {
		t := 0.3
		panelReq.Temperature = &t
	}

	// Prepend system message
	if system != "" {
		panelReq.Messages = append([]types.ChatMessage{
			{Role: "system", Content: system},
		}, req.Messages...)
	} else {
		panelReq.Messages = req.Messages
	}

	return panelReq
}

// toJudgeConfig converts skill.JudgeConfig to types.JudgeConfig.
func toJudgeConfig(jc JudgeConfig) types.JudgeConfig {
	return jc.ToBase()
}

// buildPanelOnlyResponse constructs a response with raw panel outputs (no judge).
func buildPanelOnlyResponse(name string, responses []types.PanelResponse) *types.ChatResponse {
	var b strings.Builder
	totalUsage := types.Usage{}

	for _, pr := range responses {
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

		if pr.Error == "" && !pr.TimedOut {
			totalUsage.PromptTokens += pr.Usage.PromptTokens
			totalUsage.CompletionTokens += pr.Usage.CompletionTokens
			totalUsage.TotalTokens += pr.Usage.TotalTokens
			totalUsage.CostUSD += pr.Usage.CostUSD
		}
	}

	return &types.ChatResponse{
		ID:      fmt.Sprintf("ofusion_skill_%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Model:   "openfusion/" + name,
		Choices: []types.Choice{{Index: 0, Message: types.ChatMessage{Role: "assistant", Content: strings.TrimSpace(b.String())}}},
		Usage:   totalUsage,
	}
}
