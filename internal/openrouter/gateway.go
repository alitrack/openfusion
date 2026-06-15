// Package openrouter implements an OpenRouter gateway plugin.
//
// OpenRouter has some quirks compared to standard OpenAI API:
//   - Model names are prefixed with provider (e.g. "openai/gpt-4", "anthropic/claude-3")
//   - HTTP response includes OpenRouter-specific headers (X-OpenRouter-*)
//   - Tool calls and streaming have subtle differences
//
// This plugin transparently handles these differences so OpenFusion can
// route through OpenRouter as if it were any other OpenAI-compatible provider.
package openrouter

import (
	"context"
	"fmt"
	"strings"

	"github.com/lhy/openfusion/internal/plugin"
	"github.com/lhy/openfusion/internal/types"
)

// GatewayPlugin implements plugin.ModelPlugin for OpenRouter.
// It handles model name mapping and request/response normalization.
type GatewayPlugin struct{}

// Name returns the plugin identifier.
func (p *GatewayPlugin) Name() string { return "openrouter" }

// Capabilities returns what this plugin can do.
func (p *GatewayPlugin) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{
		SupportsThinking: false,
		SupportsTools:    true,
	}
}

// TransformRequest modifies the request before sending to OpenRouter.
// Ensures model names have the provider/model format.
func (p *GatewayPlugin) TransformRequest(ctx context.Context, req *types.ChatRequest) (*types.ChatRequest, error) {
	if req.Model != "" && !strings.Contains(req.Model, "/") {
		req.Model = ResolveModel(req.Model)
	}
	return req, nil
}

// TransformResponse normalizes the response from OpenRouter.
func (p *GatewayPlugin) TransformResponse(ctx context.Context, resp *types.ChatResponse) (*types.ChatResponse, error) {
	return resp, nil
}

// ---------------------------------------------------------------------------
// Model name resolution helpers
// ---------------------------------------------------------------------------

// ResolveModel determines the correct OpenRouter model string from a short name.
// Maps common short names to their OpenRouter equivalents.
func ResolveModel(name string) string {
	// Already has provider prefix
	if strings.Contains(name, "/") {
		return name
	}

	mappings := map[string]string{
		// OpenAI
		"gpt-4":           "openai/gpt-4-turbo",
		"gpt-4o":          "openai/gpt-4o",
		"gpt-4o-mini":     "openai/gpt-4o-mini",
		"gpt-5.5":         "openai/gpt-5.5",
		"o1":              "openai/o1",
		"o3":              "openai/o3",
		"o3-mini":         "openai/o3-mini",

		// Anthropic
		"claude-opus-4.8":  "anthropic/claude-opus-4.8",
		"claude-sonnet-4.6": "anthropic/claude-sonnet-4.6",
		"claude-haiku":     "anthropic/claude-3-haiku",
		"fable-5":          "anthropic/fable-5",

		// Google
		"gemini-3-flash":   "google/gemini-3-flash",
		"gemini-3.1-pro":   "google/gemini-3.1-pro",
		"gemini-3-pro":     "google/gemini-3-pro",

		// DeepSeek
		"deepseek-v4-pro":  "deepseek/deepseek-v4-pro",
		"deepseek-v4-flash": "deepseek/deepseek-v4-flash",

		// Meta
		"llama-4":          "meta-llama/llama-4",
		"llama-4-405b":     "meta-llama/llama-4-405b",

		// Other (via OpenRouter)
		"qwen-3.5-27b":     "qwen/qwen-3.5-27b",
		"qwen-3.5-122b":    "qwen/qwen-3.5-122b",
		"kimi-k2.6":        "moonshot/kimi-k2.6",
		"glm-5.1":          "zhipu/glm-5.1",
	}

	if resolved, ok := mappings[name]; ok {
		return resolved
	}
	// Default: pass through as-is
	return name
}

// ValidateModel checks if an OpenRouter model name looks valid.
func ValidateModel(model string) error {
	if model == "" {
		return fmt.Errorf("empty model name")
	}
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid OpenRouter model format %q — expected provider/model", model)
	}
	if parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid OpenRouter model format %q — empty provider or model", model)
	}
	return nil
}

// ListModels returns a list of known OpenRouter model names.
func ListModels() []string {
	models := make([]string, 0, len(modelList))
	for _, m := range modelList {
		models = append(models, m)
	}
	return models
}

// modelList contains known OpenRouter model identifiers.
var modelList = []string{
	"openai/gpt-5.5",
	"openai/gpt-4o",
	"openai/o3",
	"anthropic/claude-opus-4.8",
	"anthropic/claude-sonnet-4.6",
	"anthropic/fable-5",
	"google/gemini-3.1-pro",
	"google/gemini-3-flash",
	"deepseek/deepseek-v4-pro",
	"deepseek/deepseek-v4-flash",
	"meta-llama/llama-4-405b",
	"qwen/qwen-3.5-122b",
	"zhipu/glm-5.1",
}
