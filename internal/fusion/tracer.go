package fusion

import "time"

// PerModelTrace records per-model performance data for a fusion run.
type PerModelTrace struct {
	Name     string        `json:"name"`
	Provider string        `json:"provider"`
	Model    string        `json:"model"`
	Layer    string        `json:"layer"`
	Latency  time.Duration `json:"latency_ms"`
	Tokens   int           `json:"tokens"`
	Error    string        `json:"error,omitempty"`
}

// MergeTraces combines multiple trace slices into one.
func MergeTraces(traces ...[]PerModelTrace) []PerModelTrace {
	var all []PerModelTrace
	for _, t := range traces {
		all = append(all, t...)
	}
	return all
}
