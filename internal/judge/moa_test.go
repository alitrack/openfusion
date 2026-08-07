package judge

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lhy/openfusion/internal/provider"
	"github.com/lhy/openfusion/internal/types"
)

// fakeProvider implements the minimal chat interface for tests.
type fakeProvider struct {
	name      string
	responses map[string]string
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) ChatCompletion(ctx context.Context, req *types.ChatRequest) (*types.ChatResponse, error) {
	// Echo the last user message back, so tests can assert on prompt composition.
	content := ""
	for _, m := range req.Messages {
		if m.Role == "user" {
			content = m.Content
		}
	}
	return &types.ChatResponse{
		Choices: []types.Choice{{Message: types.ChatMessage{Role: "assistant", Content: content}}},
	}, nil
}

// compile-time check that fakeProvider satisfies the interface used by the manager.
var _ provider.Provider = (*fakeProvider)(nil)

func newTestManager(t *testing.T, providers map[string]*fakeProvider) *provider.Manager {
	t.Helper()
	pm := provider.NewManager()
	for name, p := range providers {
		pm.Register(name, p)
	}
	return pm
}

func TestTwoTierMoA(t *testing.T) {
	pm := newTestManager(t, map[string]*fakeProvider{
		"tier1": {name: "tier1"},
		"tier2": {name: "tier2"},
	})

	s := NewSynthesizer(pm, 30*time.Second)
	panel := []types.PanelResponse{
		{Member: types.PanelMember{Provider: "tier1", Model: "m1"}, Content: "panel answer A"},
		{Member: types.PanelMember{Provider: "tier1", Model: "m2"}, Content: "panel answer B"},
	}

	res, err := s.SynthesizeWithOptions(context.Background(), types.JudgeConfig{
		Provider: "tier1",
		Model:    "judge-model",
	}, "the question?", panel, WithTwoTierMoA("tier2", "arbiter-model"))
	if err != nil {
		t.Fatalf("SynthesizeWithOptions: %v", err)
	}

	out := res.Answer
	if !strings.Contains(out, "ORIGINAL QUESTION") {
		t.Errorf("second-pass prompt missing original question: %.100s", out)
	}
	if !strings.Contains(out, "TIER-1 SYNTHESIS") {
		t.Errorf("second-pass prompt missing tier-1 synthesis marker")
	}
	if !strings.Contains(out, "RAW MODEL RESPONSES") {
		t.Errorf("second-pass prompt missing raw responses marker")
	}
	if !strings.Contains(out, "panel answer A") {
		t.Errorf("second-pass prompt missing panel response content")
	}
}

func TestTwoTierMoAWithoutOption(t *testing.T) {
	pm := newTestManager(t, map[string]*fakeProvider{
		"tier1": {name: "tier1"},
	})

	s := NewSynthesizer(pm, 30*time.Second)
	panel := []types.PanelResponse{
		{Member: types.PanelMember{Provider: "tier1", Model: "m1"}, Content: "panel answer A"},
	}

	res, err := s.SynthesizeWithOptions(context.Background(), types.JudgeConfig{
		Provider: "tier1",
		Model:    "judge-model",
	}, "the question?", panel)
	if err != nil {
		t.Fatalf("SynthesizeWithOptions: %v", err)
	}

	// Without the option, the answer should NOT contain the second-pass markers.
	if strings.Contains(res.Answer, "TIER-1 SYNTHESIS") {
		t.Errorf("two-tier ran without option: %.100s", res.Answer)
	}
}
