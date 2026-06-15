// Package plugin defines the ModelPlugin interface for model-specific optimizations.
//
// Plugins modify requests before sending to providers and responses after
// receiving them. This allows model-specific features (like DeepSeek's think
// parameter) without polluting the generic provider adapter.
package plugin

import (
	"context"

	"github.com/lhy/openfusion/internal/types"
)

// ModelPlugin modifies request/response for a specific model family.
type ModelPlugin interface {
	// Name returns a unique identifier (e.g. "deepseek", "generic").
	Name() string

	// TransformRequest modifies the request before sending.
	// Called right before the HTTP POST.
	// Return the (possibly modified) request.
	TransformRequest(ctx context.Context, req *types.ChatRequest) (*types.ChatRequest, error)

	// TransformResponse modifies the response after receiving.
	// Called right after HTTP response is parsed.
	// Return the (possibly modified) response.
	TransformResponse(ctx context.Context, resp *types.ChatResponse) (*types.ChatResponse, error)

	// Capabilities returns what this model can do, for routing decisions.
	Capabilities() Capabilities
}

// Capabilities describes a model's unique features.
type Capabilities struct {
	SupportsThinking   bool   // DeepSeek's think parameter
	SupportsVision     bool   // Image input support
	SupportsTools      bool   // Function/tool calling
	SupportsStructured bool   // JSON structured output mode
	CodexCompat        bool   // Codex-compatible structured code output
	ThinkBudgetLimit   int    // Max think budget tokens (0 = unlimited)
	MaxContextTokens   int    // Max context window size (0 = unknown)
}
