package fusion

import (
	"fmt"
	"math"

	"github.com/lhy/openfusion/internal/types"
)

// GuardsConfig holds safety limits for fusion execution.
type GuardsConfig struct {
	MaxTotalTokens     int       `yaml:"max_total_tokens" json:"max_total_tokens"`
	MaxRefTemperature  float64   `yaml:"max_ref_temperature" json:"max_ref_temperature"`
	JudgeTemperature   float64   `yaml:"judge_temperature" json:"judge_temperature"`
	FailoverAggregator *ModelRef `yaml:"failover_aggregator" json:"failover_aggregator,omitempty"`
}

// ApplyToPanel overrides temperature on a panel request.
func (g *GuardsConfig) ApplyToPanel(req *types.ChatRequest) *types.ChatRequest {
	if g == nil {
		return req
	}
	if g.MaxRefTemperature > 0 && req.Temperature != nil {
		capped := math.Min(*req.Temperature, g.MaxRefTemperature)
		req.Temperature = &capped
	}
	return req
}

// ApplyToJudge overrides temperature on a judge request.
func (g *GuardsConfig) ApplyToJudge(req *types.ChatRequest) *types.ChatRequest {
	if g == nil {
		return req
	}
	if g.JudgeTemperature > 0 {
		t := g.JudgeTemperature
		req.Temperature = &t
	}
	return req
}

// CheckBudget checks if accumulated tokens exceed the budget.
func (g *GuardsConfig) CheckBudget(accumulated int) error {
	if g == nil || g.MaxTotalTokens <= 0 {
		return nil
	}
	if accumulated > g.MaxTotalTokens {
		return fmt.Errorf("token budget exceeded: %d > %d", accumulated, g.MaxTotalTokens)
	}
	return nil
}

// HasFailover returns true if a failover aggregator is configured.
func (g *GuardsConfig) HasFailover() bool {
	return g != nil && g.FailoverAggregator != nil
}
