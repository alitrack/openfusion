package fusion

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/lhy/openfusion/internal/types"
)

// ExecuteStream runs fusion and streams the final answer as SSE events.
func (e *Engine) ExecuteStream(w http.ResponseWriter, presetName string, req *types.ChatRequest) error {
	resp, err := e.Execute(presetName, req)
	if err != nil {
		fmt.Fprintf(w, "data: {\"error\":\"%s\"}\n\n", err.Error())
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		return err
	}

	if len(resp.Choices) == 0 {
		fmt.Fprintf(w, "data: [DONE]\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		return nil
	}

	answer := resp.Choices[0].Message.Content
	created := time.Now().Unix()

	for _, ch := range answer {
		chunk := types.StreamChunk{
			ID:      resp.ID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   resp.Model,
			Choices: []types.StreamChoice{{
				Index: 0,
				Delta: types.StreamDelta{Content: string(ch)},
			}},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}

	usageJSON, _ := json.Marshal(map[string]interface{}{
		"usage": map[string]int{
			"prompt_tokens":     resp.Usage.PromptTokens,
			"completion_tokens": resp.Usage.CompletionTokens,
			"total_tokens":      resp.Usage.TotalTokens,
		},
	})
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", usageJSON)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}
