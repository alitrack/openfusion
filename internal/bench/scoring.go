// Package bench — Phase 1: scoring with HermesBench-inspired multi-axis weighting.
package bench

import (
	"math"
	"sort"
)

// ComputeVariantScore aggregates all trials for one variant into a VariantScore.
func ComputeVariantScore(variant string, trials []TrialResult) VariantScore {
	if len(trials) == 0 {
		return VariantScore{Variant: variant}
	}

	n := len(trials)
	scores := make([]float64, n)
	latencies := make([]int64, 0, n)

	var totalPanelOk, totalPanelN int
	var judgeErrors, timeouts, crashes, outcomes int
	var totalCost float64

	for i, t := range trials {
		scores[i] = float64(t.Score.Total())
		latencies = append(latencies, t.LatencyMs)

		totalPanelOk += t.PanelOk
		totalPanelN += t.PanelN
		if !t.JudgeOk {
			judgeErrors++
		}
		if t.TimedOut {
			timeouts++
		}
		if t.Crashed {
			crashes++
		}
		if DetermineGate(t) >= OutcomeReplyOnly {
			outcomes++
		}
		totalCost += t.CostUSD
	}

	avg, stddev := meanStdDev(scores)
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	// Axis scores
	capability := capabilityScore(trials)
	reliability := reliabilityScore(trials)
	efficiency := efficiencyScore(latencies)

	vs := VariantScore{
		Variant:          variant,
		AvgScore:         avg,
		StdDev:           stddev,
		MinScore:         minFloat(scores),
		MaxScore:         maxFloat(scores),
		PanelSuccessRate: safeDiv(float64(totalPanelOk), float64(totalPanelN)),
		JudgeErrorRate:   float64(judgeErrors) / float64(n),
		TimeoutRate:      float64(timeouts) / float64(n),
		CrashRate:        float64(crashes) / float64(n),
		OutcomeRate:      float64(outcomes) / float64(n),
		LatencyP50:       percentile(latencies, 0.50),
		LatencyP95:       percentile(latencies, 0.95),
		TotalCostUSD:     totalCost,
		CapabilityScore:  capability,
		ReliabilityScore: reliability,
		EfficiencyScore:  efficiency,
		GatedScore:       computeGatedScore(avg, trials),
	}

	return vs
}

// capabilityScore measures response quality: accuracy + completeness + clarity.
// 70% task fulfillment (judge scores), 30% evidence truthfulness (citation rating).
func capabilityScore(trials []TrialResult) float64 {
	if len(trials) == 0 {
		return 0
	}
	var sum float64
	for _, t := range trials {
		// Task fulfillment = avg of accuracy + completeness + clarity
		fulfill := float64(t.Score.Accuracy+t.Score.Completeness+t.Score.Clarity) / 3.0
		// Evidence truthfulness = citation rating as proxy
		evidence := float64(t.Score.CitationRating)
		sum += 0.70*fulfill + 0.30*evidence
	}
	return sum / float64(len(trials))
}

// reliabilityScore measures whether the system reaches conclusions consistently.
func reliabilityScore(trials []TrialResult) float64 {
	if len(trials) == 0 {
		return 0
	}
	var sum float64
	for _, t := range trials {
		gate := DetermineGate(t)
		switch gate {
		case OutcomeComplete:
			sum += 100
		case OutcomeUnstable:
			sum += 75
		case OutcomePartial:
			sum += 50
		case OutcomeReplyOnly:
			sum += 25
		default:
			sum += 0
		}
	}
	return sum / float64(len(trials))
}

// efficiencyScore measures responsiveness and communication quality.
func efficiencyScore(latencies []int64) float64 {
	if len(latencies) == 0 {
		return 0
	}
	// Score: 100 at p50, linearly decaying to 0 at 3× p50
	p50 := percentile(latencies, 0.50)
	if p50 == 0 {
		return 100
	}
	p95 := percentile(latencies, 0.95)
	ratio := float64(p95) / float64(p50)
	if ratio <= 1.0 {
		return 100
	}
	if ratio >= 3.0 {
		return 0
	}
	// Linear between 1.0 (100) and 3.0 (0)
	return 100 - (ratio-1.0)*50
}

// computeGatedScore applies outcome gates to the average score.
func computeGatedScore(avg float64, trials []TrialResult) float64 {
	if len(trials) == 0 {
		return 0
	}
	var gatedSum float64
	for _, t := range trials {
		gate := DetermineGate(t)
		gated, _ := ApplyGates(float64(t.Score.Total()), gate)
		gatedSum += gated
	}
	return gatedSum / float64(len(trials))
}

// meanStdDev returns mean and population standard deviation.
func meanStdDev(values []float64) (mean, stddev float64) {
	if len(values) == 0 {
		return 0, 0
	}
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))

	if len(values) == 1 {
		return mean, 0
	}

	for _, v := range values {
		stddev += (v - mean) * (v - mean)
	}
	stddev = math.Sqrt(stddev / float64(len(values)))
	return
}

// percentile returns the value at the given percentile (0.0-1.0).
func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1.0 {
		return sorted[len(sorted)-1]
	}
	idx := int(math.Round(p * float64(len(sorted)-1)))
	return sorted[idx]
}

func minFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func maxFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}
