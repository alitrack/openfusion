package fusion

import (
	"context"
	"testing"

	"github.com/lhy/openfusion/internal/plugin"
	"github.com/lhy/openfusion/internal/provider"
	"github.com/lhy/openfusion/internal/types"
)

// mockProvider implements provider.Provider for testing.
type mockProvider struct {
	name     string
	response string
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) ChatCompletion(ctx context.Context, req *types.ChatRequest) (*types.ChatResponse, error) {
	return &types.ChatResponse{
		Choices: []types.Choice{{Index: 0, Message: types.ChatMessage{Role: "assistant", Content: m.response}}},
		Usage:   types.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
	}, nil
}

func TestTopologyValidate(t *testing.T) {
	tests := []struct {
		name    string
		topo    TopologyDef
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty layers",
			topo:    TopologyDef{},
			wantErr: true,
			errMsg:  "at least one layer",
		},
		{
			name: "no judge",
			topo: TopologyDef{
				Layers: []LayerDef{
					{Name: "panel", Models: []ModelRef{{Provider: "openai", Model: "gpt-4"}}},
				},
			},
			wantErr: true,
			errMsg:  "exactly 1 judge",
		},
		{
			name: "judge not last",
			topo: TopologyDef{
				Layers: []LayerDef{
					{Name: "judge", Models: []ModelRef{{Provider: "openai", Model: "gpt-4"}}, Role: "judge"},
					{Name: "panel", Models: []ModelRef{{Provider: "openai", Model: "gpt-4"}}},
				},
			},
			wantErr: true,
			errMsg:  "must be the last layer",
		},
		{
			name: "valid 2-layer",
			topo: TopologyDef{
				Layers: []LayerDef{
					{Name: "panel", Models: []ModelRef{{Provider: "openai", Model: "gpt-4"}, {Provider: "openai", Model: "gpt-3.5"}}},
					{Name: "judge", Models: []ModelRef{{Provider: "openai", Model: "gpt-4"}}, Role: "judge"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid 3-layer",
			topo: TopologyDef{
				Layers: []LayerDef{
					{Name: "proposers", Models: []ModelRef{{Provider: "openai", Model: "gpt-4"}}},
					{Name: "critics", Models: []ModelRef{{Provider: "openai", Model: "gpt-3.5"}}},
					{Name: "aggregator", Models: []ModelRef{{Provider: "openai", Model: "gpt-4"}}, Role: "judge"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.topo.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestExecuteTopology(t *testing.T) {
	pm := provider.NewManager()
	pm.Register("mock1", &mockProvider{name: "mock1", response: "Answer from mock1"})
	pm.Register("mock2", &mockProvider{name: "mock2", response: "Answer from mock2"})

	engine := &Engine{
		defaultTimeout: 5000000000, // 5s
	}
	engine.providerMgr.Store(pm)

	topo := &TopologyDef{
		Layers: []LayerDef{
			{Name: "panel", Models: []ModelRef{
				{Provider: "mock1", Model: "mock1"},
				{Provider: "mock2", Model: "mock2"},
			}},
			{Name: "judge", Models: []ModelRef{
				{Provider: "mock1", Model: "mock1"},
			}, Role: "judge"},
		},
	}

	req := &types.ChatRequest{
		Model: "test",
		Messages: []types.ChatMessage{
			{Role: "user", Content: "test question"},
		},
	}

	ctx := context.Background()
	resp, err := engine.ExecuteTopology(ctx, topo, "test-preset", req)
	if err != nil {
		t.Fatalf("ExecuteTopology: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}

	// Verify panel responses collected
	if len(resp.PanelResponses) != 3 {
		t.Errorf("expected 3 panel responses (2 panel + 1 judge), got %d", len(resp.PanelResponses))
	}

	// verify plugin package can be imported (dependency health check)
	_ = plugin.ModelPlugin(nil)
}

func TestTopologyValidationLayerErrors(t *testing.T) {
	t.Run("empty model in layer", func(t *testing.T) {
		topo := TopologyDef{
			Layers: []LayerDef{
				{Name: "panel", Models: []ModelRef{{Provider: "", Model: ""}}},
				{Name: "judge", Models: []ModelRef{{Provider: "ok", Model: "ok"}}, Role: "judge"},
			},
		}
		err := topo.Validate()
		if err == nil {
			t.Error("expected error for empty provider/model")
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
