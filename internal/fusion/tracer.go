package fusion

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

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

// FusionTrace collects per-model performance data for a fusion run.
type FusionTrace struct {
	RequestID string
	Traces    []PerModelTrace
	Strategy  string // "layer-dag" | "layer-dag-streaming"
}

// NewTrace creates a new FusionTrace with a generated request ID.
func NewTrace(strategy string) *FusionTrace {
	return &FusionTrace{
		RequestID: fmt.Sprintf("of_%d", time.Now().UnixNano()),
		Strategy:  strategy,
	}
}

// InjectHeaders adds x-openfusion-* headers to an HTTP response.
func (ft *FusionTrace) InjectHeaders(w http.ResponseWriter) {
	var panels []string
	var judgeModel string
	var judgeLatency time.Duration
	var judgeTokens int

	for _, t := range ft.Traces {
		if t.Error != "" {
			continue
		}
		if t.Layer == "judge" {
			judgeModel = t.Name
			judgeLatency = t.Latency
			judgeTokens = t.Tokens
		} else {
			panels = append(panels, fmt.Sprintf("%s(%dms,%dt)", t.Name, t.Latency.Milliseconds(), t.Tokens))
		}
	}

	if len(panels) > 0 {
		w.Header().Set("x-openfusion-panel", strings.Join(panels, ","))
	}
	if judgeModel != "" {
		w.Header().Set("x-openfusion-judge", fmt.Sprintf("%s(%dms,%dt)", judgeModel, judgeLatency.Milliseconds(), judgeTokens))
	}
	if ft.Strategy != "" {
		w.Header().Set("x-openfusion-strategy", ft.Strategy)
	}
}
