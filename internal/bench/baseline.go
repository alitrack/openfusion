// Package bench — Phase 2d: baseline comparison against historical runs.
package bench

import "fmt"

// BaselineStats holds historical performance metrics for a variant.
type BaselineStats struct {
	Variant    string  `json:"variant"`
	Runs       int     `json:"runs"`
	AvgScore   float64 `json:"avg_score"`
	BestScore  float64 `json:"best_score"`
	WorstScore float64 `json:"worst_score"`
	AvgLatency int64   `json:"avg_latency_ms"`
}

// CompareToBaseline compares current variant scores against historical data.
// Returns a markdown comparison table and whether the current run improved.
func CompareToBaseline(current []VariantScore, store *Store) string {
	if store == nil || len(current) == 0 {
		return ""
	}

	var b string
	b += "\n## Historical Baseline Comparison\n\n"

	hasData := false
	for _, vs := range current {
		baseline := loadBaseline(store, vs.Variant, 5)
		if baseline.Runs == 0 {
			continue
		}
		hasData = true

		delta := vs.AvgScore - baseline.AvgScore
		deltaStr := fmt.Sprintf("%+.1f", delta)
		trend := "→"
		if delta > 2 {
			trend = "📈"
		} else if delta < -2 {
			trend = "📉"
		}

		b += fmt.Sprintf("| %s | %.1f | %.1f | %s | %s | %d |\n",
			vs.Variant, vs.AvgScore, baseline.AvgScore, deltaStr, trend, baseline.Runs)
	}

	if !hasData {
		return ""
	}

	b = "| Variant | Current | Baseline Avg | Δ | Trend | Historical Runs |\n" +
		"|---------|---------|-------------|------|-------|------------------|\n" + b

	return b
}

// loadBaseline computes historical baseline stats from the store.
func loadBaseline(store *Store, variant string, maxRuns int) BaselineStats {
	points, err := store.TrendForVariant(variant, maxRuns)
	if err != nil || len(points) == 0 {
		return BaselineStats{}
	}

	var sum, best, worst float64
	best = points[0].AvgScore
	worst = points[0].AvgScore

	for _, p := range points {
		sum += p.AvgScore
		if p.AvgScore > best {
			best = p.AvgScore
		}
		if p.AvgScore < worst {
			worst = p.AvgScore
		}
	}

	return BaselineStats{
		Variant:    variant,
		Runs:       len(points),
		AvgScore:   sum / float64(len(points)),
		BestScore:  best,
		WorstScore: worst,
	}
}
