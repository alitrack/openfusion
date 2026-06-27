package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/lhy/openfusion/internal/types"
)

// StreamChat implements StreamProvider for OpenAIAdapter.
func (a *OpenAIAdapter) StreamChat(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	// Apply plugin TransformRequest
	a.mu.RLock()
	plug := a.plugin
	a.mu.RUnlock()
	if plug != nil {
		var err error
		req, err = plug.TransformRequest(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("plugin TransformRequest: %w", err)
		}
	}

	// Force stream mode
	req.Stream = true

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if a.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http call: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		bodyStr := string(respBody)
		if len(bodyStr) > 512 {
			bodyStr = bodyStr[:512] + "...(truncated)"
		}
		return nil, fmt.Errorf("provider %q returned status %d: %s", a.name, resp.StatusCode, bodyStr)
	}

	ch := make(chan types.StreamChunk, 16)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		// Increase buffer for large chunks
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := strings.TrimSpace(scanner.Text())
			if line == "" || !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return
			}

			var chunk types.StreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				// Non-JSON frame — skip gracefully (some providers send keepalive comments)
				continue
			}

			ch <- chunk
		}

		if err := scanner.Err(); err != nil {
			ch <- types.StreamChunk{Error: fmt.Sprintf("stream read error: %v", err)}
		}
	}()

	return ch, nil
}

// Compile-time check: OpenAIAdapter implements StreamProvider
var _ StreamProvider = (*OpenAIAdapter)(nil)
