package fusion

import (
	"strings"
	"testing"

	"github.com/lhy/openfusion/internal/types"
)

func TestStripContext_StripNone(t *testing.T) {
	msgs := []types.ChatMessage{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
	}
	result := StripContext(msgs, StripNone)
	if len(result) != 3 {
		t.Errorf("StripNone: expected 3 messages, got %d", len(result))
	}
}

func TestStripContext_StripSystem(t *testing.T) {
	msgs := []types.ChatMessage{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
	}
	result := StripContext(msgs, StripSystem)
	if len(result) != 2 {
		t.Errorf("StripSystem: expected 2 messages, got %d", len(result))
	}
	if result[0].Role == "system" {
		t.Error("StripSystem: system message not stripped")
	}
}

func TestStripContext_StripAll(t *testing.T) {
	msgs := []types.ChatMessage{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Let me check <tool_call>search(\"test\")</tool_call>"},
		{Role: "tool", Content: "search result"},
		{Role: "assistant", Content: "Here is the answer"},
	}
	result := StripContext(msgs, StripAll)
	if len(result) != 3 {
		t.Errorf("StripAll: expected 3 messages (user + 2 assistant), got %d", len(result))
	}
	for _, m := range result {
		if m.Role == "system" || m.Role == "tool" {
			t.Errorf("StripAll: unexpected role %q", m.Role)
		}
	}
}

func TestStripContext_ToolCallRemoval(t *testing.T) {
	content := "Let me think about this.\n<tool_call>search(\"query\")</tool_call>\nBased on results, here is the answer."
	cleaned := stripToolCallsFromContent(content)
	if strings.Contains(cleaned, "tool_call") {
		t.Error("tool_call tags not stripped")
	}
	if !strings.Contains(cleaned, "Let me think") {
		t.Error("non-tool content should be preserved")
	}
}

func TestAppendToTail(t *testing.T) {
	msgs := []types.ChatMessage{
		{Role: "user", Content: "What is 2+2?"},
		{Role: "assistant", Content: "Let me think..."},
		{Role: "user", Content: "Please answer."},
	}

	outputs := []PanelResult{
		{ModelRef: ModelRef{Provider: "openai", Model: "gpt-4"}, Content: "The answer is 4."},
		{ModelRef: ModelRef{Provider: "openai", Model: "gpt-3.5"}, Content: "2+2 equals 4."},
	}

	result := AppendToTail(msgs, outputs)
	if len(result) != 3 {
		t.Errorf("expected 3 messages, got %d", len(result))
	}

	// Last user message should contain the appended outputs
	lastUser := result[2]
	if !strings.Contains(lastUser.Content, "[Reference: gpt-4]") {
		t.Error("AppendToTail: missing reference marker for gpt-4")
	}
	if !strings.Contains(lastUser.Content, "[Reference: gpt-3.5]") {
		t.Error("AppendToTail: missing reference marker for gpt-3.5")
	}
}

func TestAppendToTail_EmptyOutputs(t *testing.T) {
	msgs := []types.ChatMessage{
		{Role: "user", Content: "Hello"},
	}
	result := AppendToTail(msgs, nil)
	if len(result) != 1 {
		t.Errorf("empty outputs: expected 1 message, got %d", len(result))
	}
}

func TestAppendToTailInline(t *testing.T) {
	msgs := []types.ChatMessage{
		{Role: "user", Content: "Hello"},
	}
	outputs := []PanelResult{
		{ModelRef: ModelRef{Provider: "openai", Model: "gpt-4"}, Content: "Hi!"},
	}
	result := AppendToTailInline(msgs, outputs)
	if len(result) != 2 {
		t.Errorf("inline: expected 2 messages, got %d", len(result))
	}
	if result[1].Role != "assistant" {
		t.Error("inline: expected assistant role for appended output")
	}
}

func TestStripContext_AllModes(t *testing.T) {
	msgs := []types.ChatMessage{
		{Role: "system", Content: "prompt"},
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "<tool_call>x</tool_call>"},
		{Role: "tool", Content: "result"},
	}

	t.Run("none", func(t *testing.T) {
		r := StripContext(msgs, StripNone)
		if len(r) != 4 {
			t.Errorf("none: want 4, got %d", len(r))
		}
	})
	t.Run("system", func(t *testing.T) {
		r := StripContext(msgs, StripSystem)
		if len(r) != 3 {
			t.Errorf("system: want 3, got %d", len(r))
		}
	})
	t.Run("all", func(t *testing.T) {
		r := StripContext(msgs, StripAll)
		if len(r) != 2 {
			t.Errorf("all: want 2, got %d", len(r))
		}
	})
}

func TestAppendToTail_Truncation(t *testing.T) {
	msgs := []types.ChatMessage{
		{Role: "user", Content: "hello"},
	}

	// Create a very long content (> 16000 chars)
	longContent := strings.Repeat("x", 17000)
	outputs := []PanelResult{
		{ModelRef: ModelRef{Provider: "test", Model: "test"}, Content: longContent},
	}

	result := AppendToTail(msgs, outputs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if strings.Contains(result[0].Content, strings.Repeat("x", 17000)) {
		t.Error("long content should be truncated")
	}
	if !strings.Contains(result[0].Content, "[truncated]") {
		t.Error("truncated marker missing")
	}
}
