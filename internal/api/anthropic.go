package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/lhy/openfusion/internal/types"
)

// --- Anthropic Messages API types (request) ---

type anthropicMessagesRequest struct {
	Model       string              `json:"model"`
	MaxTokens   int                 `json:"max_tokens"`
	Messages    []anthropicMessage  `json:"messages"`
	System      string              `json:"system,omitempty"`
	Stream      bool                `json:"stream,omitempty"`
	Temperature *float64            `json:"temperature,omitempty"`
	Tools       []anthropicToolDef  `json:"tools,omitempty"`
}

type anthropicMessage struct {
	Role    string           `json:"role"`
	Content json.RawMessage  `json:"content"`
}

type anthropicToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// --- Anthropic Messages API types (response) ---

type anthropicMessagesResponse struct {
	ID         string                   `json:"id"`
	Type       string                   `json:"type"`
	Role       string                   `json:"role"`
	Model      string                   `json:"model"`
	Content    []anthropicContentBlock  `json:"content"`
	StopReason string                   `json:"stop_reason"`
	Usage      anthropicUsage           `json:"usage"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// --- Translation ---

// translateAnthropicToOpenAI converts an Anthropic Messages request to OpenAI Chat format.
func translateAnthropicToOpenAI(ar *anthropicMessagesRequest) *types.ChatRequest {
	messages := make([]types.ChatMessage, 0, len(ar.Messages)+1)

	// Add system message if present
	if ar.System != "" {
		messages = append(messages, types.ChatMessage{
			Role:    "system",
			Content: ar.System,
		})
	}

	// Translate messages
	for _, am := range ar.Messages {
		content := extractAnthropicTextContent(am.Content)
		role := am.Role
		// Anthropic uses "user"/"assistant", OpenAI same
		messages = append(messages, types.ChatMessage{
			Role:    role,
			Content: content,
		})
	}

	var temp *float64
	if ar.Temperature != nil {
		temp = ar.Temperature
	}

	return &types.ChatRequest{
		Model:       "openfusion/" + ar.Model, // use as preset name
		Messages:    messages,
		MaxTokens:   ar.MaxTokens,
		Temperature: temp,
	}
}

// extractAnthropicTextContent extracts text from Anthropic content format.
// Anthropic content is either a string or an array of {type, text} blocks.
func extractAnthropicTextContent(raw json.RawMessage) string {
	// Try string first
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str
	}

	// Try array of content blocks
	var blocks []anthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var result string
		for _, block := range blocks {
			if block.Type == "text" {
				result += block.Text
			}
		}
		return result
	}

	return string(raw)
}

// translateOpenAIToAnthropic converts an OpenAI Chat response to Anthropic Messages format.
func translateOpenAIToAnthropic(resp *types.ChatResponse, model string, promptTokens int) *anthropicMessagesResponse {
	content := ""
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
	}

	return &anthropicMessagesResponse{
		ID:   fmt.Sprintf("msg_%d", time.Now().UnixMilli()),
		Type: "message",
		Role: "assistant",
		Model: model,
		Content: []anthropicContentBlock{
			{Type: "text", Text: content},
		},
		StopReason: "end_turn",
		Usage: anthropicUsage{
			InputTokens:  promptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}
}

// --- Handler ---

func (s *Server) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	var ar anthropicMessagesRequest
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if err := json.NewDecoder(r.Body).Decode(&ar); err != nil {
		s.writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	if len(ar.Messages) == 0 {
		s.writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "messages array is empty")
		return
	}

	// Extract model/preset name (strip "openfusion/" prefix if present)
	model := ar.Model

	// Translate to OpenAI format
	req := translateAnthropicToOpenAI(&ar)

	// Route: "auto" → skill matching; "dag" → DAG decomposition; otherwise → fusion preset
	var resp *types.ChatResponse
	var err error

	switch model {
	case "auto", "openfusion/auto":
		resp, err = s.engine.ExecuteAuto(req)
	case "dag", "openfusion/dag":
		resp, err = s.engine.ExecuteDAG(req)
	default:
		// Strip openfusion/ prefix if present
		preset := model
		if len(preset) > 12 && preset[:12] == "openfusion/" {
			preset = preset[12:]
		}
		resp, err = s.engine.Execute(preset, req)
	}

	if err != nil {
		s.log.Warn("anthropic fusion failed", "model", model, "error", err.Error())
		s.writeAnthropicError(w, http.StatusInternalServerError, "api_error", err.Error())
		return
	}

	// Log fusion
	s.logFusion(model, req, resp)

	// Translate response back to Anthropic format
	promptTokens := countMessageTokens(ar.Messages, ar.System)
	anthropicResp := translateOpenAIToAnthropic(resp, model, promptTokens)

	writeJSON(w, http.StatusOK, anthropicResp)
}

func (s *Server) writeAnthropicError(w http.ResponseWriter, status int, errType, message string) {
	writeJSON(w, status, map[string]any{
		"error": anthropicError{
			Type:    errType,
			Message: message,
		},
	})
}

// countMessageTokens estimates prompt tokens for Anthropic response.
func countMessageTokens(messages []anthropicMessage, system string) int {
	total := 0
	if system != "" {
		total += len(system) / 4
	}
	for _, msg := range messages {
		total += len(extractAnthropicTextContent(msg.Content)) / 4
	}
	if total < 1 {
		return 1
	}
	return total
}
