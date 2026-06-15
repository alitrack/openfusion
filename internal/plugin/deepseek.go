package plugin

import (
	"context"

	"github.com/lhy/openfusion/internal/types"
)

// DeepSeekPlugin handles DeepSeek-specific optimizations.
// Key features:
//   - Think parameter (enables chain-of-thought reasoning)
//   - Think budget control
//   - Temperature auto-tuning
type DeepSeekPlugin struct{}

func (p *DeepSeekPlugin) Name() string { return "deepseek" }

// TransformRequest optimizes the request for DeepSeek models.
// - If skills set Think=true, it's already in ChatRequest.Think (serialized via JSON tag)
// - This plugin ensures correct defaults if Think is not explicitly set
func (p *DeepSeekPlugin) TransformRequest(ctx context.Context, req *types.ChatRequest) (*types.ChatRequest, error) {
	// If Think is explicitly set to true but no budget, set a reasonable default
	if req.Think != nil && *req.Think && req.ThinkBudget == 0 {
		req.ThinkBudget = 4096
	}
	// If temperature is unset, default to 0.3 for consistency
	if req.Temperature == nil {
		t := 0.3
		req.Temperature = &t
	}
	return req, nil
}

// TransformResponse optionally extracts DeepSeek-specific fields.
// The OpenAI adapter already parses standard fields; this could
// extract a "thinking" field from the raw response in the future.
func (p *DeepSeekPlugin) TransformResponse(ctx context.Context, resp *types.ChatResponse) (*types.ChatResponse, error) {
	return resp, nil
}

func (p *DeepSeekPlugin) Capabilities() Capabilities {
	return Capabilities{
		SupportsThinking:   true,
		SupportsVision:     false,
		SupportsTools:      true,
		SupportsStructured: true,
		CodexCompat:        true,
		ThinkBudgetLimit:   8192,
		MaxContextTokens:   131072,
	}
}
