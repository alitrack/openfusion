// Package bench — Phase 1 types: multi-trial results, harness fingerprint, outcome gates.
package bench

import "time"

// TrialResult captures a single trial execution of one task against one variant.
type TrialResult struct {
	TaskID    string `json:"task_id"`
	Trial     int    `json:"trial"`
	Variant   string `json:"variant"`
	Preset    string `json:"preset"`

	// Output
	Response string `json:"response"`
	Score    Score  `json:"score"`
	JudgeRaw string `json:"judge_raw,omitempty"`

	// Mechanical signals
	LatencyMs    int64 `json:"latency_ms"`
	TTFAMs       int64 `json:"ttfa_ms,omitempty"`     // time to first answer
	PanelOk      int   `json:"panel_ok"`               // successful panel members
	PanelN       int   `json:"panel_n"`                 // total panel members
	JudgeOk      bool  `json:"judge_ok"`               // judge completed without error
	TimedOut     bool  `json:"timed_out"`               // trial exceeded timeout
	Crashed      bool  `json:"crashed"`                 // trial ended with error

	// Cost
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd"`

	// Fusion-specific signals (populated when available from response)
	ConsensusCount      int `json:"consensus_count"`
	ContradictionCount  int `json:"contradiction_count"`
	UniqueInsightCount  int `json:"unique_insight_count"`
	BlindSpotCount      int `json:"blind_spot_count"`
	PanelMemberCount    int `json:"panel_member_count"`
	PanelMemberOk       int `json:"panel_member_ok"`
}

// VariantScore is the aggregated score for one preset across all tasks and trials.
type VariantScore struct {
	Variant    string  `json:"variant"`
	AvgScore   float64 `json:"avg_score"`
	StdDev     float64 `json:"std_dev"`
	MinScore   float64 `json:"min_score"`
	MaxScore   float64 `json:"max_score"`

	// Mechanical aggregates
	PanelSuccessRate float64 `json:"panel_success_rate"` // panel_ok / panel_n across all trials
	JudgeErrorRate   float64 `json:"judge_error_rate"`   // failed judge calls / total trials
	TimeoutRate      float64 `json:"timeout_rate"`       // timed out trials / total
	CrashRate        float64 `json:"crash_rate"`         // crashed trials / total
	OutcomeRate      float64 `json:"outcome_rate"`       // trials with valid outcome

	// Latency (ms)
	LatencyP50 int64 `json:"latency_p50"`
	LatencyP95 int64 `json:"latency_p95"`

	// Cost
	TotalCostUSD float64 `json:"total_cost_usd"`

	// Per-axis scores
	CapabilityScore   float64 `json:"capability_score"`
	ReliabilityScore  float64 `json:"reliability_score"`
	EfficiencyScore   float64 `json:"efficiency_score"`

	// Gated score (after outcome / scope gates applied)
	GatedScore float64 `json:"gated_score"`
}

// HarnessFingerprint identifies the exact configuration under test.
type HarnessFingerprint struct {
	PresetName  string   `json:"preset_name"`
	PresetHash  string   `json:"preset_hash"`
	ModelIDs    []string `json:"model_ids"`
	ProviderIDs []string `json:"provider_ids"`
	Timestamp   string   `json:"timestamp"`
	GitSHA      string   `json:"git_sha,omitempty"`
}

// RunMetadata records the context of a benchmark run.
type RunMetadata struct {
	ID            string             `json:"id"`
	Timestamp     time.Time          `json:"timestamp"`
	Fingerprint   HarnessFingerprint `json:"fingerprint"`
	Variants      []string           `json:"variants"`
	TaskCount     int                `json:"task_count"`
	TrialsPerTask int                `json:"trials_per_task"`
	Duration      time.Duration      `json:"duration"`
}

// OutcomeGate classifies the termination state of a trial.
type OutcomeGate int

const (
	OutcomeNone         OutcomeGate = iota // no response, empty, or crash
	OutcomeReplyOnly                       // got response but no true conclusion
	OutcomePartial                         // partial result (timeout after output)
	OutcomeUnstable                        // completed but high variance
	OutcomeComplete                        // clean completion
)

// ApplyGates applies outcome gates to a trial score.
// Returns the gated score and the gate that was applied.
func ApplyGates(score float64, gate OutcomeGate) (float64, OutcomeGate) {
	switch gate {
	case OutcomeNone:
		return 0, OutcomeNone
	case OutcomeReplyOnly:
		if score > 30 {
			return 30, OutcomeReplyOnly
		}
		return score, OutcomeReplyOnly
	case OutcomePartial:
		if score > 50 {
			return 50, OutcomePartial
		}
		return score, OutcomePartial
	case OutcomeUnstable:
		if score > 75 {
			return 75, OutcomeUnstable
		}
		return score, OutcomeUnstable
	case OutcomeComplete:
		return score, OutcomeComplete
	default:
		return score, OutcomeComplete
	}
}

// DetermineGate classifies a trial result into an outcome gate.
func DetermineGate(r TrialResult) OutcomeGate {
	if r.Crashed {
		return OutcomeNone
	}
	if r.TimedOut {
		if r.Response != "" && r.Response != "(empty)" {
			return OutcomePartial
		}
		return OutcomeNone
	}
	if r.Response == "" || r.Response == "(empty)" {
		return OutcomeNone
	}
	if !r.JudgeOk {
		return OutcomeReplyOnly
	}
	if r.Score.Total() == 0 {
		return OutcomeReplyOnly
	}
	return OutcomeComplete
}

// FlatScore combines all dimensions into one number using HermesBench-inspired weights.
// Weights: capability(0.60) + reliability(0.25) + efficiency(0.15)
func FlatScore(capability, reliability, efficiency float64) float64 {
	return 0.60*capability + 0.25*reliability + 0.15*efficiency
}
