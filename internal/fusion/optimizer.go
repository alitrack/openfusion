package fusion

import (
	"fmt"
	"strings"

	"github.com/lhy/openfusion/internal/types"
)

// StripMode controls what gets removed from panel model calls.
type StripMode int

const (
	StripNone   StripMode = iota // send full context
	StripSystem                   // remove system prompt
	StripAll                      // remove system + tool calls (MoA 2.0 mode)
)

// OptimizerConfig controls context optimization for panel calls.
type OptimizerConfig struct {
	StripMode  StripMode `yaml:"strip_mode" json:"strip_mode"`
	AppendMode string    `yaml:"append_mode" json:"append_mode"` // "tail" | "inline"
}

// StripContext removes specified content types from messages.
// Returns a NEW slice; original is untouched.
func StripContext(msgs []types.ChatMessage, mode StripMode) []types.ChatMessage {
	var out []types.ChatMessage
	for _, m := range msgs {
		switch mode {
		case StripNone:
			out = append(out, m)
		case StripSystem:
			if m.Role != "system" {
				out = append(out, m)
			}
		case StripAll:
			switch m.Role {
			case "system", "tool":
				continue // skip
			case "assistant":
				// Strip tool_calls from assistant messages
				cleaned := m
				cleaned.Content = stripToolCallsFromContent(m.Content)
				out = append(out, cleaned)
			default:
				out = append(out, m)
			}
		}
	}
	return out
}

// stripToolCallsFromContent removes tool call XML/JSON blocks from assistant content.
func stripToolCallsFromContent(content string) string {
	// Strip <tool_call>...</tool_call> blocks (common XML format)
	for {
		start := strings.Index(content, "<tool_call>")
		if start == -1 {
			break
		}
		end := strings.Index(content, "</tool_call>")
		if end == -1 {
			break
		}
		content = content[:start] + content[end+len("</tool_call>"):]
	}
	// Strip {"tool_calls":...} blocks (JSON format) - simplified
	content = stripJSONToolCalls(content)
	return strings.TrimSpace(content)
}

func stripJSONToolCalls(content string) string {
	// Simple heuristic: remove lines starting with {"tool_calls":
	lines := strings.Split(content, "\n")
	var filtered []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, `{"tool_calls":`) {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}

// AppendToTail appends panel outputs to the last user message's content (MoA 2.0 pattern).
func AppendToTail(messages []types.ChatMessage, panelOutputs []PanelResult) []types.ChatMessage {
	out := make([]types.ChatMessage, len(messages))
	copy(out, messages)

	if len(panelOutputs) == 0 {
		return out
	}

	// Find last user message
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role == "user" {
			var sb strings.Builder
			sb.WriteString(out[i].Content)
			for _, po := range panelOutputs {
				if po.Error != "" {
					continue
				}
				content := po.Content
				if len(content) > 16000 {
					content = content[:16000] + "...[truncated]"
				}
				sb.WriteString(fmt.Sprintf("\n\n---\n[Reference: %s]\n%s", po.ModelRef.Model, content))
			}
			out[i].Content = sb.String()
			break
		}
	}

	return out
}

// AppendToTailInline appends panel outputs as new assistant messages (legacy mode).
func AppendToTailInline(messages []types.ChatMessage, panelOutputs []PanelResult) []types.ChatMessage {
	out := make([]types.ChatMessage, len(messages))
	copy(out, messages)

	for _, po := range panelOutputs {
		if po.Error != "" {
			continue
		}
		out = append(out, types.ChatMessage{
			Role:    "assistant",
			Content: fmt.Sprintf("[%s]: %s", po.ModelRef.Model, po.Content),
		})
	}

	return out
}
