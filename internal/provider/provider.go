// Package provider defines the interface for LLM backends and provides built-in adapters.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/lhy/openfusion/internal/plugin"
	"github.com/lhy/openfusion/internal/types"
)

// Provider is the interface that all LLM backends must implement.
type Provider interface {
	// ChatCompletion sends a chat request and returns the response.
	ChatCompletion(ctx context.Context, req *types.ChatRequest) (*types.ChatResponse, error)

	// Name returns the provider name (e.g. "openai", "openrouter").
	Name() string
}

// ---------------------------------------------------------------------------
// Manager
// ---------------------------------------------------------------------------

// Manager holds registered provider instances.
type Manager struct {
	providers map[string]Provider
	httpClient *http.Client
}

// NewManager creates a provider manager with the given HTTP client.
func NewManager() *Manager {
	return &Manager{
		providers: make(map[string]Provider),
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Register adds a provider by name.
func (m *Manager) Register(name string, p Provider) {
	m.providers[name] = p
}

// Get returns a provider by name.
func (m *Manager) Get(name string) (Provider, error) {
	p, ok := m.providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// OpenAI-compatible adapter (also used for OpenRouter, DeepSeek, Ollama)
// ---------------------------------------------------------------------------

// OpenAIAdapter calls any OpenAI-compatible /v1/chat/completions endpoint.
type OpenAIAdapter struct {
	name    string
	baseURL string
	apiKey  string
	client  *http.Client
	plugin  plugin.ModelPlugin
}

// NewOpenAIAdapter creates an adapter for an OpenAI-compatible provider.
func NewOpenAIAdapter(name, baseURL, apiKey string) *OpenAIAdapter {
	return &OpenAIAdapter{
		name:    name,
		baseURL: baseURL,
		apiKey:  apiKey,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// SetPlugin attaches a model plugin to this adapter.
func (a *OpenAIAdapter) SetPlugin(p plugin.ModelPlugin) {
	a.plugin = p
}

// Plugin returns the attached plugin, or nil.
func (a *OpenAIAdapter) Plugin() plugin.ModelPlugin {
	return a.plugin
}

// Name returns the provider name.
func (a *OpenAIAdapter) Name() string { return a.name }

// ChatCompletion sends a chat request to an OpenAI-compatible endpoint.
func (a *OpenAIAdapter) ChatCompletion(ctx context.Context, req *types.ChatRequest) (*types.ChatResponse, error) {
	// Apply plugin TransformRequest
	if a.plugin != nil {
		var err error
		req, err = a.plugin.TransformRequest(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("plugin TransformRequest: %w", err)
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if a.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider %q returned status %d: %s", a.name, resp.StatusCode, string(respBody))
	}

	var chatResp types.ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	chatResp.Model = req.Model

	// Apply plugin TransformResponse
	if a.plugin != nil {
		chatRespPtr, err := a.plugin.TransformResponse(ctx, &chatResp)
		if err != nil {
			return nil, fmt.Errorf("plugin TransformResponse: %w", err)
		}
		return chatRespPtr, nil
	}

	return &chatResp, nil
}
