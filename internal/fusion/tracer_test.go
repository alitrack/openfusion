package fusion

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTracer_InjectHeaders(t *testing.T) {
	trace := &FusionTrace{
		RequestID: "test-123",
		Strategy:  "layer-dag",
		Traces: []PerModelTrace{
			{Name: "deepseek-v4", Provider: "deepseek", Model: "deepseek-chat", Layer: "panel", Latency: 234 * time.Millisecond, Tokens: 1200},
			{Name: "gpt-5.5", Provider: "openai", Model: "gpt-5.5", Layer: "panel", Latency: 312 * time.Millisecond, Tokens: 1450},
			{Name: "claude-opus", Provider: "anthropic", Model: "claude-opus-4.8", Layer: "judge", Latency: 845 * time.Millisecond, Tokens: 2100},
		},
	}

	w := httptest.NewRecorder()
	trace.InjectHeaders(w)

	panel := w.Header().Get("x-openfusion-panel")
	if !strings.Contains(panel, "deepseek-v4(234ms,1200t)") {
		t.Errorf("panel header missing deepseek: %s", panel)
	}
	if !strings.Contains(panel, "gpt-5.5(312ms,1450t)") {
		t.Errorf("panel header missing gpt-5.5: %s", panel)
	}

	judge := w.Header().Get("x-openfusion-judge")
	if !strings.Contains(judge, "claude-opus(845ms,2100t)") {
		t.Errorf("judge header wrong: %s", judge)
	}

	strategy := w.Header().Get("x-openfusion-strategy")
	if strategy != "layer-dag" {
		t.Errorf("strategy = %q, want layer-dag", strategy)
	}
}

func TestTracer_InjectHeaders_WithErrors(t *testing.T) {
	trace := &FusionTrace{
		Strategy: "layer-dag",
		Traces: []PerModelTrace{
			{Name: "good-model", Provider: "openai", Layer: "panel", Latency: 100 * time.Millisecond, Tokens: 500},
			{Name: "broken-model", Provider: "broken", Layer: "panel", Error: "timeout"},
			{Name: "judge-model", Provider: "anthropic", Layer: "judge", Latency: 500 * time.Millisecond, Tokens: 1000},
		},
	}

	w := httptest.NewRecorder()
	trace.InjectHeaders(w)

	panel := w.Header().Get("x-openfusion-panel")
	if strings.Contains(panel, "broken-model") {
		t.Error("error models should be excluded from panel header")
	}
	if !strings.Contains(panel, "good-model") {
		t.Error("successful models should appear")
	}

	judge := w.Header().Get("x-openfusion-judge")
	if !strings.Contains(judge, "judge-model") {
		t.Error("judge header missing")
	}
}
