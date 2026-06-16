// Package provider defines the interface for LLM backends and provides built-in adapters.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/lhy/openfusion/internal/logger"
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
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewManager creates a provider manager.
func NewManager() *Manager {
	return &Manager{
		providers: make(map[string]Provider),
	}
}

// Register adds a provider by name.
func (m *Manager) Register(name string, p Provider) {
	m.mu.Lock()
	m.providers[name] = p
	m.mu.Unlock()
}

// Get returns a provider by name.
func (m *Manager) Get(name string) (Provider, error) {
	m.mu.RLock()
	p, ok := m.providers[name]
	m.mu.RUnlock()
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
	mu      sync.RWMutex
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
	a.mu.Lock()
	a.plugin = p
	a.mu.Unlock()
}

// Plugin returns the attached plugin, or nil.
func (a *OpenAIAdapter) Plugin() plugin.ModelPlugin {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.plugin
}

// Name returns the provider name.
func (a *OpenAIAdapter) Name() string { return a.name }

// ChatCompletion sends a chat request to an OpenAI-compatible endpoint.
func (a *OpenAIAdapter) ChatCompletion(ctx context.Context, req *types.ChatRequest) (*types.ChatResponse, error) {
	// Apply plugin TransformRequest — single RLock acquisition for both plugin checks
	a.mu.RLock()
	plug := a.plugin
	a.mu.RUnlock()
	if plug != nil {
		var err error
		req, err = plug.TransformRequest(ctx, req)
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
		// Truncate body to avoid leaking secrets/account info in logs
		bodyStr := string(respBody)
		if len(bodyStr) > 512 {
			bodyStr = bodyStr[:512] + "...(truncated)"
		}
		logger.Debug("provider non-200 response body", "provider", a.name, "status", fmt.Sprintf("%d", resp.StatusCode), "body", bodyStr)
		logger.Warn("provider returned non-200", "provider", a.name, "status", fmt.Sprintf("%d", resp.StatusCode))
		return nil, fmt.Errorf("provider %q returned status %d", a.name, resp.StatusCode)
	}

	var chatResp types.ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	chatResp.Model = req.Model

	// Apply plugin TransformResponse — reuse plug from single RLock above
	if plug != nil {
		chatRespPtr, err := plug.TransformResponse(ctx, &chatResp)
		if err != nil {
			return nil, fmt.Errorf("plugin TransformResponse: %w", err)
		}
		return chatRespPtr, nil
	}

	return &chatResp, nil
}
