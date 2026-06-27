package fusion

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lhy/openfusion/internal/provider"
	"github.com/lhy/openfusion/internal/types"
)

// mockStreamProvider implements provider.StreamProvider for testing.
type mockStreamProvider struct {
	name     string
	chunks   []types.StreamChunk
	delay    time.Duration
}

func (m *mockStreamProvider) Name() string { return m.name }
func (m *mockStreamProvider) ChatCompletion(ctx context.Context, req *types.ChatRequest) (*types.ChatResponse, error) {
	var content string
	for _, c := range m.chunks {
		for _, ch := range c.Choices {
			content += ch.Delta.Content
		}
	}
	return &types.ChatResponse{
		Choices: []types.Choice{{Index: 0, Message: types.ChatMessage{Role: "assistant", Content: content}}},
	}, nil
}
func (m *mockStreamProvider) StreamChat(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	ch := make(chan types.StreamChunk, len(m.chunks))
	go func() {
		defer close(ch)
		for _, c := range m.chunks {
			select {
			case <-ctx.Done():
				return
			case <-time.After(m.delay):
			}
			ch <- c
		}
	}()
	return ch, nil
}

func TestStreamMerger_ExecuteLayerStream(t *testing.T) {
	pm := provider.NewManager()
	pm.Register("mock1", &mockStreamProvider{
		name: "mock1",
		chunks: []types.StreamChunk{
			{Choices: []types.StreamChoice{{Delta: types.StreamDelta{Content: "Hello "}}}},
			{Choices: []types.StreamChoice{{Delta: types.StreamDelta{Content: "world"}}}},
		},
	})

	w := httptest.NewRecorder()
	merger, err := NewStreamMerger(w)
	if err != nil {
		t.Fatalf("NewStreamMerger: %v", err)
	}

	layer := LayerDef{
		Name: "panel",
		Models: []ModelRef{
			{Provider: "mock1", Model: "mock1"},
		},
	}

	msgs := []types.ChatMessage{{Role: "user", Content: "hi"}}
	results, traces := merger.ExecuteLayerStream(context.Background(), pm, layer, msgs)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", results[0].Content)
	}
	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}
	if traces[0].Tokens != 2 {
		t.Errorf("expected 2 tokens, got %d", traces[0].Tokens)
	}
}

func TestStreamMerger_MultipleLayers(t *testing.T) {
	pm := provider.NewManager()
	pm.Register("mock1", &mockStreamProvider{
		name: "mock1",
		chunks: []types.StreamChunk{
			{Choices: []types.StreamChoice{{Delta: types.StreamDelta{Content: "A"}}}},
		},
		delay: 5 * time.Millisecond,
	})
	pm.Register("mock2", &mockStreamProvider{
		name: "mock2",
		chunks: []types.StreamChunk{
			{Choices: []types.StreamChoice{{Delta: types.StreamDelta{Content: "B"}}}},
		},
		delay: 10 * time.Millisecond,
	})

	w := httptest.NewRecorder()
	merger, _ := NewStreamMerger(w)

	layer1 := LayerDef{Name: "proposers", Models: []ModelRef{
		{Provider: "mock1", Model: "mock1"},
		{Provider: "mock2", Model: "mock2"},
	}}

	msgs := []types.ChatMessage{{Role: "user", Content: "q"}}
	results1, traces1 := merger.ExecuteLayerStream(context.Background(), pm, layer1, msgs)

	if len(results1) != 2 {
		t.Fatalf("layer1: expected 2 results, got %d", len(results1))
	}
	if len(traces1) != 2 {
		t.Fatalf("layer1: expected 2 traces, got %d", len(traces1))
	}
}

func TestStreamMerger_ProviderError(t *testing.T) {
	pm := provider.NewManager()
	pm.Register("broken", &mockStreamProvider{
		name: "broken",
		// StreamChat returns nil for chunks — this provider works but the name is wrong for lookup
	})
	// Don't register — will fail on lookup

	w := httptest.NewRecorder()
	merger, _ := NewStreamMerger(w)

	layer := LayerDef{Name: "panel", Models: []ModelRef{
		{Provider: "nonexistent", Model: "nonexistent"},
	}}

	results, traces := merger.ExecuteLayerStream(context.Background(), pm, layer, nil)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Error == "" {
		t.Error("expected error for nonexistent provider")
	}
	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}
	if traces[0].Error == "" {
		t.Error("expected error in trace")
	}
}

func TestStreamMerger_NonStreamingFallback(t *testing.T) {
	// A provider that implements Provider but NOT StreamProvider
	pm := provider.NewManager()
	pm.Register("basic", &mockProvider{name: "basic", response: "fallback ok"})

	w := httptest.NewRecorder()
	merger, _ := NewStreamMerger(w)

	layer := LayerDef{Name: "panel", Models: []ModelRef{
		{Provider: "basic", Model: "basic"},
	}}

	results, _ := merger.ExecuteLayerStream(context.Background(), pm, layer, nil)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != "fallback ok" {
		t.Errorf("expected 'fallback ok', got %q", results[0].Content)
	}
}

func TestStreamMerger_EmitDone(t *testing.T) {
	w := httptest.NewRecorder()
	merger, _ := NewStreamMerger(w)

	traces := []PerModelTrace{
		{Name: "gpt-5.5", Provider: "openai", Model: "gpt-5.5", Layer: "panel", Latency: 234 * time.Millisecond, Tokens: 1200},
		{Name: "claude", Provider: "anthropic", Model: "claude", Layer: "judge", Latency: 845 * time.Millisecond, Tokens: 2100},
	}

	merger.EmitDone(traces, "layer-dag-streaming")

	body := w.Body.String()
	if !strings.Contains(body, "[DONE]") {
		t.Error("missing [DONE] marker")
	}

	// Parse the done event
	lines := strings.Split(body, "\n")
	var lastData string
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") && !strings.HasPrefix(line, "data: [DONE]") {
			lastData = strings.TrimPrefix(line, "data: ")
		}
	}

	var done map[string]interface{}
	if err := json.Unmarshal([]byte(lastData), &done); err != nil {
		t.Fatalf("unmarshal done: %v", err)
	}

	panel, ok := done["x_openfusion_panel"].([]interface{})
	if !ok || len(panel) != 1 {
		t.Errorf("expected 1 panel trace, got %v", done["x_openfusion_panel"])
	}
	judge, ok := done["x_openfusion_judge"].(string)
	if !ok || !strings.Contains(judge, "claude") {
		t.Errorf("expected judge trace, got %v", done["x_openfusion_judge"])
	}
}

func TestMergeTraces(t *testing.T) {
	a := []PerModelTrace{{Name: "A"}, {Name: "B"}}
	b := []PerModelTrace{{Name: "C"}}
	merged := MergeTraces(a, b)
	if len(merged) != 3 {
		t.Errorf("expected 3 traces, got %d", len(merged))
	}
}
