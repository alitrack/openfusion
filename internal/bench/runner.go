// Package bench — Phase 1: upgraded runner with multi-trial, latency, and mechanical signals.
package bench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/lhy/openfusion/internal/types"
)

// RunnerConfig controls benchmark execution.
type RunnerConfig struct {
	TrialsPerTask int           // ≥1, default 2
	PanelTimeout  time.Duration // per-panel-member timeout
	JudgeTimeout  time.Duration // per-judge call timeout
}

// DefaultRunnerConfig returns sensible defaults.
func DefaultRunnerConfig() RunnerConfig {
	return RunnerConfig{
		TrialsPerTask: 2,
		PanelTimeout:  30 * time.Second,
		JudgeTimeout:  60 * time.Second,
	}
}

// FusionExecutor abstracts how a preset executes a chat request.
// This decouples the bench runner from the specific engine implementation.
type FusionExecutor interface {
	Execute(presetName string, req *types.ChatRequest) (*types.ChatResponse, error)
}

// JudgeExecutor abstracts the judge model used for scoring.
type JudgeExecutor interface {
	ChatCompletion(ctx context.Context, req *types.ChatRequest) (*types.ChatResponse, error)
}

// RunPreset executes all tasks against one preset with multi-trial and returns results.
func RunPreset(
	executor FusionExecutor,
	judge JudgeExecutor,
	presetName string,
	ts *TestSet,
	cfg RunnerConfig,
) ([]TrialResult, []string) {
	var allTrials []TrialResult
	modelIDs := make(map[string]bool)

	for _, task := range ts.Tasks {
		for trial := 0; trial < cfg.TrialsPerTask; trial++ {
			result := runOneTrial(executor, judge, presetName, task, trial, cfg)
			allTrials = append(allTrials, result)

			// Track model IDs from result (if available)
			if result.Preset != "" {
				modelIDs[result.Preset] = true
			}
		}
	}

	ids := make([]string, 0, len(modelIDs))
	for id := range modelIDs {
		ids = append(ids, id)
	}
	return allTrials, ids
}

func runOneTrial(
	executor FusionExecutor,
	judge JudgeExecutor,
	presetName string,
	task Task,
	trial int,
	cfg RunnerConfig,
) TrialResult {
	ctx := context.Background()
	start := time.Now()

	req := &types.ChatRequest{
		Model: "openfusion/" + presetName,
		Messages: []types.ChatMessage{
			{Role: "user", Content: task.Prompt},
		},
	}

	result := TrialResult{
		TaskID:  task.ID,
		Trial:   trial,
		Variant: presetName,
		Preset:  presetName,
		PanelN:  1,
	}

	resp, err := executor.Execute(presetName, req)
	result.LatencyMs = time.Since(start).Milliseconds()

	if err != nil {
		result.Crashed = true
		result.Response = fmt.Sprintf("ERROR: %v", err)
		return result
	}

	if len(resp.Choices) == 0 {
		result.Crashed = true
		result.Response = "(empty)"
		return result
	}

	result.Response = resp.Choices[0].Message.Content
	result.PromptTokens = resp.Usage.PromptTokens
	result.CompletionTokens = resp.Usage.CompletionTokens
	result.TotalTokens = resp.Usage.TotalTokens

	// Estimate cost from tokens when CostUSD isn't set by provider
	result.CostUSD = resp.Usage.CostUSD
	if result.CostUSD == 0 && result.TotalTokens > 0 {
		// DeepSeek V4 Pro: $0.14/M input, $0.28/M output (approximate)
		costPerM := 0.21 // blended average
		result.CostUSD = float64(result.TotalTokens) * costPerM / 1_000_000
	}

	// Mechanical: extract panel info from response metadata
	if resp.Analysis != nil {
		result.ConsensusCount = len(resp.Analysis.Consensus)
		result.ContradictionCount = len(resp.Analysis.Contradictions)
		result.UniqueInsightCount = len(resp.Analysis.UniqueInsights)
		result.BlindSpotCount = len(resp.Analysis.BlindSpots)
	}
	if len(resp.PanelResponses) > 0 {
		result.PanelMemberCount = len(resp.PanelResponses)
		result.PanelOk = 0
		result.PanelN = len(resp.PanelResponses)
		var totalCost float64
		var maxLatency int64
		for _, pr := range resp.PanelResponses {
			if pr.Error == "" {
				result.PanelOk++
			}
			totalCost += pr.CostUSD
			if pr.DurationMs > maxLatency {
				maxLatency = pr.DurationMs
			}
		}
		result.CostUSD = totalCost
		// Use max panel latency as mechanical signal
		if result.LatencyMs == 0 && maxLatency > 0 {
			result.LatencyMs = maxLatency
		}
	} else {
		result.PanelOk = 1
		result.PanelN = 1
		if resp.Usage.CostUSD > 0 {
			result.CostUSD = resp.Usage.CostUSD
		}
	}

	// Judge the response
	judgePrompt := BuildJudgePrompt(task, result.Response)
	judgeReq := &types.ChatRequest{
		Model: "deepseek-chat",
		Messages: []types.ChatMessage{
			{Role: "user", Content: judgePrompt},
		},
	}

	judgeResp, err := judge.ChatCompletion(ctx, judgeReq)
	if err != nil {
		result.JudgeOk = false
		result.Score = Score{Notes: fmt.Sprintf("judge error: %v", err)}
		return result
	}

	if len(judgeResp.Choices) == 0 {
		result.JudgeOk = false
		result.Score = Score{Notes: "judge returned empty"}
		return result
	}

	result.JudgeOk = true
	result.JudgeRaw = judgeResp.Choices[0].Message.Content

	score, err := ExtractScore(result.JudgeRaw)
	if err != nil {
		result.Score = Score{Notes: fmt.Sprintf("parse: %v (raw: %.200s)", err, result.JudgeRaw)}
		return result
	}

	result.Score = score
	return result
}

// FingerprintPreset creates a harness fingerprint for a preset.
func FingerprintPreset(presetName string, modelIDs []string) HarnessFingerprint {
	hash := sha256.Sum256([]byte(presetName))
	return HarnessFingerprint{
		PresetName: presetName,
		PresetHash: hex.EncodeToString(hash[:8]),
		ModelIDs:   modelIDs,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}
}

// NewRunID generates a unique run ID from timestamp.
func NewRunID() string {
	return "run_" + time.Now().UTC().Format("20060102T150405")
}

// FormatComparisonReport generates a HermesBench-style comparison report.
func FormatComparisonReport(desc string, variants []string, allTrials []TrialResult) string {
	// Group trials by variant
	byVariant := make(map[string][]TrialResult)
	for _, t := range allTrials {
		byVariant[t.Variant] = append(byVariant[t.Variant], t)
	}

	variantScores := make([]VariantScore, 0, len(variants))
	for _, v := range variants {
		vs := ComputeVariantScore(v, byVariant[v])
		variantScores = append(variantScores, vs)
	}

	var b string
	b += fmt.Sprintf("# Benchmark: %s\n\n", desc)
	b += fmt.Sprintf("Variants: %s | Trials per task: %d\n\n", join(variants, ", "), len(byVariant[variants[0]])/10)

	// Quality comparison table
	b += "## Quality (LLM Judge)\n\n"
	b += "| Variant | Avg | StdDev | Capability | Reliability | Efficiency | Gated |\n"
	b += "|---------|-----|--------|------------|-------------|-----------|-------|\n"
	for _, vs := range variantScores {
		b += fmt.Sprintf("| %s | %.1f | ±%.1f | %.0f | %.0f | %.0f | %.0f |\n",
			vs.Variant, vs.AvgScore, vs.StdDev,
			vs.CapabilityScore, vs.ReliabilityScore, vs.EfficiencyScore, vs.GatedScore)
	}

	// Mechanical signals
	b += "\n## Mechanical Signals\n\n"
	b += "| Variant | Panel OK% | Judge Err% | Timeout% | Crash% | Outcome% | P50 | P95 |\n"
	b += "|---------|-----------|------------|----------|--------|----------|-----|-----|\n"
	for _, vs := range variantScores {
		b += fmt.Sprintf("| %s | %.0f%% | %.0f%% | %.0f%% | %.0f%% | %.0f%% | %dms | %dms |\n",
			vs.Variant,
			vs.PanelSuccessRate*100, vs.JudgeErrorRate*100,
			vs.TimeoutRate*100, vs.CrashRate*100,
			vs.OutcomeRate*100,
			vs.LatencyP50, vs.LatencyP95)
	}

	// Fusion-specific signals (only shown when data available)
	hasFusion := false
	for _, t := range allTrials {
		if t.ConsensusCount > 0 || t.ContradictionCount > 0 || t.BlindSpotCount > 0 {
			hasFusion = true
			break
		}
	}
	if hasFusion {
		b += "\n## Fusion Signals\n\n"
		b += "| Variant | Consensus | Contradictions | Unique Insights | Blind Spots | Panel Members |\n"
		b += "|---------|-----------|---------------|-----------------|-------------|---------------|\n"
		for _, v := range variants {
			trials := byVariant[v]
			var cSum, ctSum, uSum, bSum, pSum int
			for _, t := range trials {
				cSum += t.ConsensusCount
				ctSum += t.ContradictionCount
				uSum += t.UniqueInsightCount
				bSum += t.BlindSpotCount
				pSum += t.PanelMemberCount
			}
			n := len(trials)
			b += fmt.Sprintf("| %s | %.1f | %.1f | %.1f | %.1f | %.1f |\n",
				v,
				float64(cSum)/float64(n), float64(ctSum)/float64(n),
				float64(uSum)/float64(n), float64(bSum)/float64(n),
				float64(pSum)/float64(n))
		}
	}

	// Cost
	b += "\n## Cost\n\n"
	b += "| Variant | Total Cost | Cost/Task |\n"
	b += "|---------|------------|----------|\n"
	for _, vs := range variantScores {
		b += fmt.Sprintf("| %s | $%.4f | $%.4f |\n",
			vs.Variant, vs.TotalCostUSD, vs.TotalCostUSD/float64(len(byVariant[vs.Variant])))
	}

	// Fusion lift (when exactly 2 variants)
	if len(variantScores) == 2 {
		baseline := variantScores[0]
		candidate := variantScores[1]
		lift := candidate.GatedScore - baseline.GatedScore
		b += "\n## Fusion Lift\n\n"
		b += fmt.Sprintf("| %s (baseline) | %s (fusion) | Δ Gated | Δ Capability |\n", baseline.Variant, candidate.Variant)
		b += "|------|------|------|------|\n"
		b += fmt.Sprintf("| %.1f | %.1f | %+.1f | %+.0f |\n",
			baseline.GatedScore, candidate.GatedScore, lift,
			candidate.CapabilityScore-baseline.CapabilityScore)
		if lift > 1 {
			b += "\n✅ Fusion significantly improves over baseline"
		} else if lift > 0 {
			b += "\n✅ Fusion marginally improves over baseline"
		} else {
			b += "\n⚠️  Fusion does NOT improve over baseline"
		}
	}

	return b
}

func join(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	r := ss[0]
	for _, s := range ss[1:] {
		r += sep + s
	}
	return r
}
