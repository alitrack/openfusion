// Package e2e provides an end-to-end integration test for the full OpenFusion pipeline.
package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lhy/openfusion/internal/api"
	"github.com/lhy/openfusion/internal/fusion"
	"github.com/lhy/openfusion/internal/preset"
	"github.com/lhy/openfusion/internal/provider"
	"github.com/lhy/openfusion/internal/types"
)

// mockProvider returns fixed responses for testing.
type mockProvider struct {
	name string
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) ChatCompletion(ctx context.Context, req *types.ChatRequest) (*types.ChatResponse, error) {
	return &types.ChatResponse{
		ID:     "mock_" + m.name,
		Object: "chat.completion",
		Choices: []types.Choice{
			{Index: 0, Message: types.ChatMessage{Role: "assistant", Content: "Mock answer from " + m.name}},
		},
		Usage: types.Usage{PromptTokens: 5, CompletionTokens: 10, TotalTokens: 15, CostUSD: 0.001},
	}, nil
}

func TestFullPipeline(t *testing.T) {
	// Setup: temp preset file
	dir := t.TempDir()
	presetContent := `
name: test-combo
description: E2E test preset
panel:
  - provider: mock-a
    model: model-alpha
    system: "Be helpful."
  - provider: mock-b
    model: model-beta
    system: "Be thorough."
judge:
  provider: mock-judge
  model: judge-model
`
	if err := os.WriteFile(filepath.Join(dir, "test-combo.yaml"), []byte(presetContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Provider manager with mock providers
	pm := provider.NewManager()
	pm.Register("mock-a", &mockProvider{name: "mock-a"})
	pm.Register("mock-b", &mockProvider{name: "mock-b"})
	pm.Register("mock-judge", &mockProvider{name: "mock-judge"})

	// Preset registry
	pr := preset.NewRegistry()
	if err := pr.LoadDir(dir); err != nil {
		t.Fatal(err)
	}

	// Fusion engine
	engine := fusion.NewEngine(pr, pm, 5, 10, 30, nil, nil, nil, nil, nil, nil, nil)

	// HTTP API server
	srv := api.NewServer(engine, "", ":0", nil, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	// Test: GET /v1/models
	t.Run("GET /v1/models", func(t *testing.T) {
		resp, err := http.Get(httpSrv.URL + "/v1/models")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}

		var body map[string]any
		json.NewDecoder(resp.Body).Decode(&body)
		data := body["data"].([]any)
		found := false
		for _, item := range data {
			m := item.(map[string]any)
			if m["id"] == "openfusion/test-combo" {
				found = true
				break
			}
		}
		if !found {
			t.Error("openfusion/test-combo not found in model list")
		}
	})

	// Test: POST /v1/chat/completions
	t.Run("POST /v1/chat/completions", func(t *testing.T) {
		payload := `{"model":"openfusion/test-combo","messages":[{"role":"user","content":"What is Go?"}]}`
		resp, err := http.Post(httpSrv.URL+"/v1/chat/completions", "application/json", strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200. Body: %s", resp.StatusCode, func() string {
				var m map[string]string
				json.NewDecoder(resp.Body).Decode(&m)
				return m["error"]
			}())
		}

		var chatResp types.ChatResponse
		if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if chatResp.Object != "chat.completion" {
			t.Errorf("Object = %q, want %q", chatResp.Object, "chat.completion")
		}
		if len(chatResp.Choices) != 1 {
			t.Fatalf("choices = %d, want 1", len(chatResp.Choices))
		}
		if chatResp.Choices[0].Message.Content == "" {
			t.Error("content is empty")
		}
		if chatResp.Usage.TotalTokens == 0 {
			t.Error("usage.total_tokens is 0")
		}
	})
}
