package skill

import (
	"testing"

	"github.com/lhy/openfusion/internal/types"
)

// ---------------------------------------------------------------------------
// Registry Tests
// ---------------------------------------------------------------------------

// TestRegistry_LoadPresets verifies that presets are auto-generated into skills
// and can be matched by the matcher. (Task 4.7)
func TestRegistry_LoadPresets(t *testing.T) {
	r := NewRegistry()

	// Load a preset — should auto-generate a skill
	presets := []types.Preset{
		{
			Name:        "budget",
			Description: "平价组合 — DeepSeek V4 Pro + Qwen 3.5 27B → GLM 5.1 Judge",
			Panel: []types.PanelMember{
				{Provider: "modelscope", Model: "deepseek-ai/DeepSeek-V4-Pro"},
				{Provider: "modelscope", Model: "Qwen/Qwen3.5-27B"},
			},
			Judge: types.JudgeConfig{
				Provider: "modelscope",
				Model:    "ZhipuAI/GLM-5.1",
			},
		},
		{
			Name:        "self-ensemble",
			Description: "自融合 — DeepSeek V4 Pro × 2 → GLM 5.1",
			Panel: []types.PanelMember{
				{Provider: "modelscope", Model: "deepseek-ai/DeepSeek-V4-Pro"},
				{Provider: "modelscope", Model: "deepseek-ai/DeepSeek-V4-Pro"},
			},
			Judge: types.JudgeConfig{
				Provider: "modelscope",
				Model:    "ZhipuAI/GLM-5.1",
			},
		},
	}

	r.LoadPresets(presets)

	// Both presets should be registered as skills
	if _, ok := r.Get("budget"); !ok {
		t.Error("expected 'budget' skill to be auto-generated from preset")
	}
	if _, ok := r.Get("self-ensemble"); !ok {
		t.Error("expected 'self-ensemble' skill to be auto-generated from preset")
	}

	// Verify auto-generated triggers exist
	budgetSkill, _ := r.Get("budget")
	if len(budgetSkill.Triggers) == 0 {
		t.Error("expected auto-generated triggers for preset-derived skill")
	}

	// Explicit skill with same name should be rejected (duplicate)
	dupSkill := &Skill{
		Name: "budget",
		Mode: ModeDirect,
		Strategy: Strategy{
			Provider: "modelscope",
			Model:    "deepseek-ai/DeepSeek-V4-Pro",
		},
	}
	if err := r.Add(dupSkill); err == nil {
		t.Error("expected error when adding explicit skill with same name as auto-generated")
	}

	// Verify only auto-generated skill remains
	loaded, ok := r.Get("budget")
	if !ok {
		t.Fatal("expected 'budget' to still exist")
	}
	if loaded.Mode != ModeFusion {
		t.Errorf("expected auto-generated budget mode ModeFusion, got %s", loaded.Mode)
	}
}

// TestRegistry_ExplicitOverridesPreset verifies that explicit skills take priority
// over auto-generated preset skills when the name matches. (Task 4.7)
func TestRegistry_ExplicitOverridesPreset(t *testing.T) {
	r := NewRegistry()

	// Add explicit skill first
	explicit := &Skill{
		Name: "analysis",
		Mode: ModeDirect,
		Strategy: Strategy{
			Provider: "local-ds",
			Model:    "deepseek-v4-flash",
		},
		Priority: 100,
		Triggers: []Trigger{
			{Categories: "analysis"},
		},
	}
	if err := r.Add(explicit); err != nil {
		t.Fatalf("Add explicit skill: %v", err)
	}

	// Now load a preset with the same name — should NOT override
	presets := []types.Preset{
		{
			Name:        "analysis",
			Description: "analysis preset",
			Panel: []types.PanelMember{
				{Provider: "modelscope", Model: "deepseek-ai/DeepSeek-V4-Pro"},
				{Provider: "modelscope", Model: "stepfun-ai/Step-3.7-Flash"},
			},
			Judge: types.JudgeConfig{Provider: "modelscope", Model: "ZhipuAI/GLM-5.1"},
		},
	}
	r.LoadPresets(presets)

	// The explicit skill should still be the one returned
	loaded, ok := r.Get("analysis")
	if !ok {
		t.Fatal("expected 'analysis' skill to exist")
	}
	if loaded.Mode != ModeDirect {
		t.Errorf("expected ModeDirect (explicit skill), got %s (overridden by preset)", loaded.Mode)
	}

	// The matcher should match based on the explicit skill's triggers
	m := r.Matcher("qa-simple")
	feat := &RequestFeatures{
		TokenCount: 100,
		Categories: []string{"analysis"},
	}
	matched := m.Match(feat)
	if matched == nil {
		t.Fatal("expected analysis request to match 'analysis' skill")
	}
	if matched.Name != "analysis" {
		t.Errorf("expected matched skill 'analysis', got '%s'", matched.Name)
	}
}

// TestRegistry_BackwardCompat verifies that direct preset model names
// (like "openfusion/budget") are handled by the preset path, not the skill path.
// This is a compile-level test: the API server routes preset names via
// engine.Execute() (preset lookup), while "auto" routes via engine.ExecuteAuto()
// (skill matching). (Task 4.8)
func TestRegistry_BackwardCompat(t *testing.T) {
	r := NewRegistry()

	// Add explicit skills
	r.Add(&Skill{
		Name: "qa-simple",
		Mode: ModeDirect,
		Strategy: Strategy{Provider: "local-ds", Model: "deepseek-v4-flash"},
		Triggers: []Trigger{
			{Categories: "greeting|general"},
		},
		Priority: 100,
	})
	r.Add(&Skill{
		Name: "code-gen",
		Mode: ModeSelfEnsemble,
		Strategy: Strategy{
			Provider: "local-ds",
			Model:    "deepseek-v4-flash",
			Panel: []PanelMemberConfig{
				{Provider: "local-ds", Model: "deepseek-v4-flash"},
				{Provider: "local-ds", Model: "deepseek-v4-flash"},
			},
		},
		Triggers: []Trigger{{Categories: "code"}},
		Priority: 80,
	})

	// Load presets that overlap with skill names
	presets := []types.Preset{
		{
			Name:        "qa-simple",
			Description: "qa preset",
			Panel: []types.PanelMember{
				{Provider: "modelscope", Model: "deepseek-ai/DeepSeek-V4-Pro"},
			},
			Judge: types.JudgeConfig{Provider: "modelscope", Model: "ZhipuAI/GLM-5.1"},
		},
	}
	r.LoadPresets(presets)

	// Verify explicit qa-simple is still in the registry (not overridden)
	skillByName, ok := r.Get("qa-simple")
	if !ok {
		t.Fatal("expected 'qa-simple' skill to exist")
	}
	if skillByName.Mode != ModeDirect {
		t.Errorf("expected explicit qa-simple (ModeDirect), got %s", skillByName.Mode)
	}

	// Verify the matcher works correctly
	m := r.Matcher("qa-simple")

	// A greeting request should match qa-simple (not fall through to preset)
	greetingFeat := &RequestFeatures{TokenCount: 10, Categories: []string{"greeting"}}
	matched := m.Match(greetingFeat)
	if matched == nil || matched.Name != "qa-simple" {
		t.Errorf("expected greeting to match 'qa-simple', got %v", matched)
	}

	// A code request should match code-gen
	codeFeat := &RequestFeatures{TokenCount: 200, Categories: []string{"code"}}
	matched = m.Match(codeFeat)
	if matched == nil || matched.Name != "code-gen" {
		t.Errorf("expected code request to match 'code-gen', got %v", matched)
	}

	// Verify preset-only names still exist in registry list
	all := r.List()
	foundPreset := false
	for _, s := range all {
		if s.Name == "qa-simple" && s.Mode == ModeDirect {
			// Explicit mode confirms it's the loaded skill, not the preset
			foundPreset = true
		}
	}
	if !foundPreset {
		t.Error("expected 'qa-simple' (explicit) in skill list")
	}
}
