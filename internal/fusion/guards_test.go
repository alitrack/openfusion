package fusion

import (
	"testing"

	"github.com/lhy/openfusion/internal/types"
)

func TestGuardsConfig_ApplyToPanel(t *testing.T) {
	cfg := &GuardsConfig{MaxRefTemperature: 0.6}
	temperature := 0.9
	req := &types.ChatRequest{Temperature: &temperature}
	cfg.ApplyToPanel(req)
	if *req.Temperature != 0.6 {
		t.Errorf("expected 0.6, got %f", *req.Temperature)
	}
}

func TestGuardsConfig_ApplyToPanel_NilConfig(t *testing.T) {
	var cfg *GuardsConfig
	temperature := 0.9
	req := &types.ChatRequest{Temperature: &temperature}
	result := cfg.ApplyToPanel(req)
	if *result.Temperature != 0.9 {
		t.Error("nil config should not modify temperature")
	}
}

func TestGuardsConfig_ApplyToJudge(t *testing.T) {
	cfg := &GuardsConfig{JudgeTemperature: 0.4}
	req := &types.ChatRequest{}
	cfg.ApplyToJudge(req)
	if req.Temperature == nil || *req.Temperature != 0.4 {
		t.Errorf("expected 0.4, got %v", req.Temperature)
	}
}

func TestGuardsConfig_CheckBudget_Exceeded(t *testing.T) {
	cfg := &GuardsConfig{MaxTotalTokens: 1000}
	err := cfg.CheckBudget(1500)
	if err == nil {
		t.Error("expected error for exceeded budget")
	}
}

func TestGuardsConfig_CheckBudget_OK(t *testing.T) {
	cfg := &GuardsConfig{MaxTotalTokens: 1000}
	err := cfg.CheckBudget(500)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGuardsConfig_CheckBudget_Disabled(t *testing.T) {
	cfg := &GuardsConfig{MaxTotalTokens: 0}
	err := cfg.CheckBudget(999999)
	if err != nil {
		t.Errorf("disabled budget should not error: %v", err)
	}
}

func TestGuardsConfig_HasFailover(t *testing.T) {
	t.Run("with failover", func(t *testing.T) {
		cfg := &GuardsConfig{FailoverAggregator: &ModelRef{Provider: "openai", Model: "gpt-4"}}
		if !cfg.HasFailover() {
			t.Error("expected HasFailover=true")
		}
	})
	t.Run("without failover", func(t *testing.T) {
		cfg := &GuardsConfig{}
		if cfg.HasFailover() {
			t.Error("expected HasFailover=false")
		}
	})
}
