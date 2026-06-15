package openrouter

import (
	"context"
	"testing"

	"github.com/lhy/openfusion/internal/types"
)

func TestResolveModel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"gpt-4o", "openai/gpt-4o"},
		{"claude-opus-4.8", "anthropic/claude-opus-4.8"},
		{"deepseek-v4-pro", "deepseek/deepseek-v4-pro"},
		{"already/provider", "already/provider"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		got := ResolveModel(tt.input)
		if got != tt.want {
			t.Errorf("ResolveModel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestValidateModel(t *testing.T) {
	if err := ValidateModel("openai/gpt-4"); err != nil {
		t.Errorf("expected valid: %v", err)
	}
	if err := ValidateModel("barename"); err == nil {
		t.Error("expected error for bare model name")
	}
	if err := ValidateModel(""); err == nil {
		t.Error("expected error for empty model name")
	}
}

func TestGatewayPlugin_Name(t *testing.T) {
	p := &GatewayPlugin{}
	if p.Name() != "openrouter" {
		t.Errorf("expected name 'openrouter', got %s", p.Name())
	}
}

func TestGatewayPlugin_TransformRequest(t *testing.T) {
	ctx := context.Background()
	p := &GatewayPlugin{}

	// Bare model → prefixed
	req := &types.ChatRequest{Model: "gpt-4"}
	result, err := p.TransformRequest(ctx, req)
	if err != nil {
		t.Fatalf("TransformRequest error: %v", err)
	}
	if result.Model != "openai/gpt-4-turbo" {
		t.Errorf("expected model 'openai/gpt-4-turbo', got %s", result.Model)
	}

	// Already prefixed → unchanged
	req2 := &types.ChatRequest{Model: "anthropic/claude-opus-4.8"}
	result2, _ := p.TransformRequest(ctx, req2)
	if result2.Model != "anthropic/claude-opus-4.8" {
		t.Errorf("expected model unchanged, got %s", result2.Model)
	}

	// Unknown → pass through
	req3 := &types.ChatRequest{Model: "unknown-model"}
	result3, _ := p.TransformRequest(ctx, req3)
	if result3.Model != "unknown-model" {
		t.Errorf("expected model 'unknown-model' passed through, got %s", result3.Model)
	}
}

func TestListModels(t *testing.T) {
	models := ListModels()
	if len(models) == 0 {
		t.Error("expected non-empty model list")
	}
	found := false
	for _, m := range models {
		if m == "openai/gpt-5.5" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected openai/gpt-5.5 in model list")
	}
}
