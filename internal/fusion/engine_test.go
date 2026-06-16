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
	e := &Engine{}
	e.presetRegistry.Store(preset.NewRegistry())
	presets := e.ListPresets()
	if len(presets) != 0 {
		t.Errorf("empty registry: got %d presets, want 0", len(presets))
	}
}

// ---------------------------------------------------------------------------
// Preset override tests
// ---------------------------------------------------------------------------

func deepCopyPresetForTest(p *types.Preset) *types.Preset {
	return applyPresetOverrides(p, nil, nil)
}

func TestApplyPresetOverrides_NoOverride(t *testing.T) {
	orig := &types.Preset{Name: "test", Panel: []types.PanelMember{{Provider: "a", Model: "b"}}}
	cp := applyPresetOverrides(orig, nil, nil)
	if cp != orig {
		t.Error("nil overrides should return original pointer")
	}
}

func TestApplyPresetOverrides_PanelOverride(t *testing.T) {
	orig := &types.Preset{Name: "test", Panel: []types.PanelMember{{Provider: "a", Model: "b"}}}
	override := []types.PanelMember{{Provider: "c", Model: "d"}}
	cp := applyPresetOverrides(orig, override, nil)
	if cp == orig {
		t.Error("override should return new preset")
	}
	if len(cp.Panel) != 1 || cp.Panel[0].Provider != "c" {
		t.Errorf("panel override failed: got %+v", cp.Panel)
	}
	// Original should be untouched
	if len(orig.Panel) != 1 || orig.Panel[0].Provider != "a" {
		t.Error("original preset was mutated")
	}
}

func TestApplyPresetOverrides_JudgeOverride(t *testing.T) {
	orig := &types.Preset{
		Name: "test",
		Judge: types.JudgeConfig{Provider: "a", Model: "b"},
	}
	override := &types.JudgeConfig{Provider: "c", Model: "d"}
	cp := applyPresetOverrides(orig, nil, override)
	if cp.Judge.Provider != "c" || cp.Judge.Model != "d" {
		t.Errorf("judge override failed: got %+v", cp.Judge)
	}
	if orig.Judge.Provider != "a" {
		t.Error("original preset judge was mutated")
	}
}

func TestApplyPresetOverrides_PartialPanelOnly(t *testing.T) {
	orig := &types.Preset{
		Name:  "test",
		Panel: []types.PanelMember{{Provider: "a", Model: "b"}},
		Judge: types.JudgeConfig{Provider: "x", Model: "y"},
	}
	override := []types.PanelMember{{Provider: "c", Model: "d"}}
	cp := applyPresetOverrides(orig, override, nil)
	if len(cp.Panel) != 1 || cp.Panel[0].Provider != "c" {
		t.Error("panel should be overridden")
	}
	if cp.Judge.Provider != "x" || cp.Judge.Model != "y" {
		t.Error("judge should remain from preset")
	}
}

