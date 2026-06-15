package plugin

import (
	"context"
	"testing"

	"github.com/lhy/openfusion/internal/types"
)

func TestRegisterAndGet(t *testing.T) {
	// Reset registry for test isolation
	registry = map[string]ModelPlugin{}
	defer func() { registry = map[string]ModelPlugin{} }()

	p := &GenericPlugin{}
	Register(p)

	got := Get("generic")
	if got == nil {
		t.Fatal("expected generic plugin to be found")
	}
	if got.Name() != "generic" {
		t.Errorf("expected name 'generic', got %s", got.Name())
	}
}

func TestRegisterDuplicate(t *testing.T) {
	registry = map[string]ModelPlugin{}
	defer func() { registry = map[string]ModelPlugin{} }()

	Register(&GenericPlugin{})
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate register")
		}
	}()
	Register(&GenericPlugin{})
}

func TestGetUnknown(t *testing.T) {
	registry = map[string]ModelPlugin{}
	defer func() { registry = map[string]ModelPlugin{} }()

	got := Get("nonexistent")
	if got != nil {
		t.Errorf("expected nil for unknown plugin, got %v", got)
	}
}

func TestList(t *testing.T) {
	registry = map[string]ModelPlugin{}
	defer func() { registry = map[string]ModelPlugin{} }()

	Register(&GenericPlugin{})
	Register(&DeepSeekPlugin{})

	names := List()
	if len(names) != 2 {
		t.Errorf("expected 2 plugins, got %d: %v", len(names), names)
	}
}

func TestGenericPluginPassthrough(t *testing.T) {
	p := &GenericPlugin{}
	ctx := context.Background()

	req := &types.ChatRequest{Model: "test"}
	outReq, err := p.TransformRequest(ctx, req)
	if err != nil {
		t.Fatalf("TransformRequest error: %v", err)
	}
	if outReq != req {
		t.Error("expected same request pointer for no-op")
	}

	resp := &types.ChatResponse{Model: "test"}
	outResp, err := p.TransformResponse(ctx, resp)
	if err != nil {
		t.Fatalf("TransformResponse error: %v", err)
	}
	if outResp != resp {
		t.Error("expected same response pointer for no-op")
	}

	caps := p.Capabilities()
	if caps.SupportsThinking {
		t.Error("generic plugin should not support thinking")
	}
}

func TestDeepSeekPlugin(t *testing.T) {
	p := &DeepSeekPlugin{}
	ctx := context.Background()

	// Test: Think=true without budget → sets default budget
	think := true
	req := &types.ChatRequest{
		Model: "deepseek-v4-flash",
		Think: &think,
	}
	outReq, err := p.TransformRequest(ctx, req)
	if err != nil {
		t.Fatalf("TransformRequest error: %v", err)
	}
	if outReq.ThinkBudget != 4096 {
		t.Errorf("expected ThinkBudget=4096 when nil, got %d", outReq.ThinkBudget)
	}

	// Test: No temperature set → defaults to 0.3
	if outReq.Temperature == nil {
		t.Error("expected Temperature to be set by default")
	} else if *outReq.Temperature != 0.3 {
		t.Errorf("expected Temperature=0.3, got %f", *outReq.Temperature)
	}

	// Test: Existing budget preserved
	temp2 := 0.1
	think2 := true
	req2 := &types.ChatRequest{
		Model:       "deepseek-v4-flash",
		Think:       &think2,
		ThinkBudget: 8192,
		Temperature: &temp2,
	}
	outReq2, _ := p.TransformRequest(ctx, req2)
	if outReq2.ThinkBudget != 8192 {
		t.Errorf("expected ThinkBudget preserved at 8192, got %d", outReq2.ThinkBudget)
	}
	if *outReq2.Temperature != 0.1 {
		t.Errorf("expected Temperature preserved at 0.1, got %f", *outReq2.Temperature)
	}

	// Test: Capabilities
	caps := p.Capabilities()
	if !caps.SupportsThinking {
		t.Error("DeepSeek should support thinking")
	}
	if caps.ThinkBudgetLimit != 8192 {
		t.Errorf("expected ThinkBudgetLimit=8192, got %d", caps.ThinkBudgetLimit)
	}
	if caps.MaxContextTokens != 131072 {
		t.Errorf("expected MaxContextTokens=131072, got %d", caps.MaxContextTokens)
	}
}
