package fusion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/lhy/openfusion/internal/provider"
	"github.com/lhy/openfusion/internal/types"
)

// StreamMerger orchestrates SSE streaming fusion: panel SSE → buffer → judge SSE → client.
type StreamMerger struct {
	output      http.ResponseWriter
	flusher     http.Flusher
	mu          sync.Mutex
	trace       []PerModelTrace
}

// NewStreamMerger creates a merger that writes SSE to the given writer.
func NewStreamMerger(w http.ResponseWriter) (*StreamMerger, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming requires http.Flusher support")
	}
	return &StreamMerger{output: w, flusher: flusher}, nil
}

// ExecuteLayerStream dispatches all models in a layer in parallel, collecting SSE streams.
// Returns merged text for the layer, plus per-model traces.
func (sm *StreamMerger) ExecuteLayerStream(
	ctx context.Context,
	pm *provider.Manager,
	layer LayerDef,
	messages []types.ChatMessage,
) ([]StreamPanelResult, []PerModelTrace) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var results []StreamPanelResult
	var traces []PerModelTrace

	for _, model := range layer.Models {
		wg.Add(1)
		go func(m ModelRef) {
			defer wg.Done()

			result := StreamPanelResult{ModelRef: m}
			trace := PerModelTrace{
				Name:     m.Model,
				Provider: m.Provider,
				Model:    m.Model,
				Layer:    layer.Name,
			}
			start := time.Now()

			prov, err := pm.Get(m.Provider)
			if err != nil {
				result.Error = err.Error()
				trace.Error = err.Error()
				trace.Latency = time.Since(start)
				mu.Lock()
				results = append(results, result)
				traces = append(traces, trace)
				mu.Unlock()
				return
			}

			req := &types.ChatRequest{
				Model:    m.Model,
				Messages: messages,
				Stream:   true,
			}

			// Use streaming provider if available
			streamer, ok := prov.(provider.StreamProvider)
			if !ok {
				// Fallback: non-streaming call
				resp, err := prov.ChatCompletion(ctx, req)
				trace.Latency = time.Since(start)
				if err != nil {
					result.Error = err.Error()
					trace.Error = err.Error()
				} else if len(resp.Choices) > 0 {
					result.Content = resp.Choices[0].Message.Content
					trace.Tokens = resp.Usage.TotalTokens
				}
				mu.Lock()
				results = append(results, result)
				traces = append(traces, trace)
				mu.Unlock()
				return
			}

			// Streaming path
			chunks, err := streamer.StreamChat(ctx, req)
			if err != nil {
				result.Error = err.Error()
				trace.Error = err.Error()
				trace.Latency = time.Since(start)
				mu.Lock()
				results = append(results, result)
				traces = append(traces, trace)
				mu.Unlock()
				return
			}

			// Collect chunks
			var content string
			var tokCount int
			for chunk := range chunks {
				if chunk.Error != "" {
					result.Error = chunk.Error
					trace.Error = chunk.Error
					break
				}
				for _, choice := range chunk.Choices {
					content += choice.Delta.Content
				}
				tokCount++
			}
			result.Content = content
			trace.Tokens = tokCount
			trace.Latency = time.Since(start)

			mu.Lock()
			results = append(results, result)
			traces = append(traces, trace)
			mu.Unlock()
		}(model)
	}

	wg.Wait()
	return results, traces
}

// StreamJudge streams the judge model's output to the client via SSE.
func (sm *StreamMerger) StreamJudge(
	ctx context.Context,
	pm *provider.Manager,
	model ModelRef,
	judgeMessages []types.ChatMessage,
) (*StreamPanelResult, error) {
	prov, err := pm.Get(model.Provider)
	if err != nil {
		return nil, fmt.Errorf("judge provider: %w", err)
	}

	req := &types.ChatRequest{
		Model:    model.Model,
		Messages: judgeMessages,
		Stream:   true,
	}

	streamer, ok := prov.(provider.StreamProvider)
	if !ok {
		// Fallback: non-streaming judge
		resp, err := prov.ChatCompletion(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("judge completion: %w", err)
		}
		if len(resp.Choices) == 0 {
			return nil, fmt.Errorf("judge returned empty response")
		}
		return &StreamPanelResult{
			ModelRef: model,
			Content:  resp.Choices[0].Message.Content,
		}, nil
	}

	// Streaming judge — emit SSE to client
	chunks, err := streamer.StreamChat(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("judge stream: %w", err)
	}

	var fullContent string
	for chunk := range chunks {
		if chunk.Error != "" {
			return nil, fmt.Errorf("judge stream error: %s", chunk.Error)
		}
		for _, choice := range chunk.Choices {
			fullContent += choice.Delta.Content
		}
		// Forward to client
		sm.writeSSEChunk(chunk)
	}

	return &StreamPanelResult{
		ModelRef: model,
		Content:  fullContent,
	}, nil
}

// writeSSEChunk writes an OpenAI-format stream chunk to the SSE response.
func (sm *StreamMerger) writeSSEChunk(chunk types.StreamChunk) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	data, _ := json.Marshal(chunk)
	fmt.Fprintf(sm.output, "data: %s\n\n", data)
	sm.flusher.Flush()
}

// EmitDone sends the final SSE done event with trace headers.
func (sm *StreamMerger) EmitDone(traces []PerModelTrace, strategy string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Build trace headers
	var panelParts []string
	var judgePart string
	for _, t := range traces {
		if t.Error != "" {
			continue
		}
		if t.Layer == "judge" {
			judgePart = fmt.Sprintf("%s(%dms,%dt)", t.Name, t.Latency.Milliseconds(), t.Tokens)
		} else {
			panelParts = append(panelParts, fmt.Sprintf("%s(%dms,%dt)", t.Name, t.Latency.Milliseconds(), t.Tokens))
		}
	}

	done := map[string]interface{}{
		"usage": map[string]int{"total_tokens": 0}, // placeholder
	}
	if len(panelParts) > 0 {
		done["x_openfusion_panel"] = panelParts
	}
	if judgePart != "" {
		done["x_openfusion_judge"] = judgePart
	}
	done["x_openfusion_strategy"] = strategy

	data, _ := json.Marshal(done)
	fmt.Fprintf(sm.output, "data: %s\n\ndata: [DONE]\n\n", data)
	sm.flusher.Flush()
}

// EmitError sends an SSE error event and closes the stream.
func (sm *StreamMerger) EmitError(err error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	errData, _ := json.Marshal(map[string]string{"error": err.Error()})
	fmt.Fprintf(sm.output, "event: error\ndata: %s\n\n", errData)
	sm.flusher.Flush()
}

// StreamPanelResult holds the collected streaming output from one panel model.
type StreamPanelResult struct {
	ModelRef ModelRef
	Content  string
	Tokens   int
	Error    string
}
