// Package fusion implements model routing heuristics based on request complexity.
package fusion

import (
	"strings"

	"github.com/lhy/openfusion/internal/types"
)

// ModelRouter selects panel/judge configurations based on request complexity.
// Uses a 5-dimension heuristic inspired by OpenFang's routing.rs:
//  1. Token estimate (chars/4)
//  2. Code markers detection
//  3. Conversation depth (message count)
//  4. System prompt length
//  5. Tool count
type ModelRouter struct {
	config types.RouterConfig
}

// NewModelRouter creates a ModelRouter with the given configuration.
func NewModelRouter(cfg types.RouterConfig) *ModelRouter {
	return &ModelRouter{config: cfg}
}

// codeMarkers are substrings that indicate a code-related request.
var codeMarkers = []string{
	"```",
	"func ",
	"def ",
	"class ",
	"import ",
	"package ",
	"fn ",
	"public class",
	"function ",
	"const ",
	"let ",
	"var ",
	"#include",
	"SELECT ",
	"FROM ",
	"WHERE ",
	"docker",
	"kubectl",
	"git ",
	"npm ",
	"pip ",
	"cargo ",
}

// Score computes the 5-dimension heuristic score for a chat request.
// Higher scores indicate more complex requests.
func (r *ModelRouter) Score(req *types.ChatRequest) types.RoutingMetrics {
	metrics := types.RoutingMetrics{}

	// Dimension 1: Token estimate (chars/4)
	totalChars := 0
	for _, msg := range req.Messages {
		totalChars += len(msg.Content)
	}
	metrics.TokenEstimate = totalChars / 4

	// Dimension 2: Code markers detection
	codeCount := 0
	for _, msg := range req.Messages {
		lower := strings.ToLower(msg.Content)
		for _, marker := range codeMarkers {
			codeCount += strings.Count(lower, strings.ToLower(marker))
		}
	}
	metrics.CodeMarkers = codeCount

	// Dimension 3: Conversation depth
	metrics.ConversationDepth = len(req.Messages)

	// Dimension 4: System prompt length
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			metrics.SystemPromptLen = len(msg.Content)
			break
		}
	}

	// Dimension 5: Tool count
	metrics.ToolCount = len(req.Tools)

	// Composite score: weighted sum normalized to [0, 1]
	metrics.Score = r.computeScore(metrics)
	metrics.Complexity = r.classifyComplexity(metrics.Score)

	return metrics
}

// computeScore calculates a weighted composite score from routing metrics.
// Weights are tuned to produce a score roughly in [0, 1]:
//   - TokenEstimate:   0.00005 per token  (20000 tokens → 1.0)
//   - CodeMarkers:     0.05 per marker    (20 markers → 1.0)
//   - ConvDepth:       0.02 per message   (50 messages → 1.0)
//   - SysPromptLen:    0.00005 per char   (20000 chars → 1.0)
//   - ToolCount:       0.1 per tool       (10 tools → 1.0)
func (r *ModelRouter) computeScore(m types.RoutingMetrics) float64 {
	score := 0.0
	score += float64(m.TokenEstimate) * 0.00005
	score += float64(m.CodeMarkers) * 0.05
	score += float64(m.ConversationDepth) * 0.02
	score += float64(m.SystemPromptLen) * 0.00005
	score += float64(m.ToolCount) * 0.1
	return score
}

// classifyComplexity maps a score to a complexity tier.
func (r *ModelRouter) classifyComplexity(score float64) string {
	if score <= r.config.SimpleThreshold {
		return "simple"
	}
	if score >= r.config.ComplexThreshold {
		return "complex"
	}
	return "medium"
}

// SelectPreset chooses panel and judge configurations based on request complexity.
// Falls back to the Medium tier if a tier's panel/judge is not fully configured.
func (r *ModelRouter) SelectPreset(req *types.ChatRequest) (panel []types.PanelMember, judge types.JudgeConfig) {
	metrics := r.Score(req)
	return r.selectByComplexity(metrics.Complexity)
}

// SelectPresetByComplexity chooses panel and judge for a given complexity string.
func (r *ModelRouter) SelectPresetByComplexity(complexity string) (panel []types.PanelMember, judge types.JudgeConfig) {
	return r.selectByComplexity(complexity)
}

func (r *ModelRouter) selectByComplexity(complexity string) (panel []types.PanelMember, judge types.JudgeConfig) {
	switch complexity {
	case "simple":
		panel = r.config.SimplePanel
		judge = r.config.SimpleJudge
		if len(panel) == 0 || judge.Model == "" {
			// Fall back to medium
			panel = r.config.MediumPanel
			judge = r.config.MediumJudge
		}
	case "complex":
		panel = r.config.ComplexPanel
		judge = r.config.ComplexJudge
		if len(panel) == 0 || judge.Model == "" {
			panel = r.config.MediumPanel
			judge = r.config.MediumJudge
		}
	default: // "medium"
		panel = r.config.MediumPanel
		judge = r.config.MediumJudge
	}

	// Ultimate fallback: if medium is also empty, return empty config
	return panel, judge
}

// DefaultRouterConfig returns a sensible default RouterConfig.
func DefaultRouterConfig() types.RouterConfig {
	return types.RouterConfig{
		SimpleThreshold:  0.3,
		ComplexThreshold: 0.7,
	}
}
