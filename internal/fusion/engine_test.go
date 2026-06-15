package fusion

import (
	"testing"

	"github.com/lhy/openfusion/internal/preset"
	"github.com/lhy/openfusion/internal/types"
)

func TestExtractLastUserMessage(t *testing.T) {
	tests := []struct {
		name     string
		messages []types.ChatMessage
		want     string
	}{
		{
			name: "single user message",
			messages: []types.ChatMessage{
				{Role: "user", Content: "hello"},
			},
			want: "hello",
		},
		{
			name: "system + user",
			messages: []types.ChatMessage{
				{Role: "system", Content: "be helpful"},
				{Role: "user", Content: "what is X"},
			},
			want: "what is X",
		},
		{
			name: "multi-turn",
			messages: []types.ChatMessage{
				{Role: "user", Content: "first"},
				{Role: "assistant", Content: "response"},
				{Role: "user", Content: "follow-up"},
			},
			want: "follow-up",
		},
		{
			name: "no user messages",
			messages: []types.ChatMessage{
				{Role: "system", Content: "be helpful"},
			},
			want: "",
		},
		{
			name:     "empty",
			messages: []types.ChatMessage{},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := types.ExtractLastUserMessage(tt.messages)
			if got != tt.want {
				t.Errorf("ExtractLastUserMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestListPresets(t *testing.T) {
	e := &Engine{presetRegistry: preset.NewRegistry()}
	presets := e.ListPresets()
	if len(presets) != 0 {
		t.Errorf("empty registry: got %d presets, want 0", len(presets))
	}
}
