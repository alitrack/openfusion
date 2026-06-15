package skill

import (
	"testing"
)

func TestCodeGenSkillParse(t *testing.T) {
	skill, err := LoadFile("../../skills/code-gen.skill.yaml")
	if err != nil {
		t.Fatalf("load code-gen skill: %v", err)
	}
	t.Logf("Skill: %s (mode=%s)", skill.Name, skill.Mode)
	t.Logf("Panel count: %d", len(skill.Strategy.Panel))
	for i, pm := range skill.Strategy.Panel {
		t.Logf("  panel[%d]: provider=%q model=%q think=%v temp=%v",
			i, pm.Provider, pm.Model, pm.Think, pm.Temperature)
	}
	t.Logf("Judge: provider=%q model=%q enabled=%v",
		skill.Strategy.Judge.Provider, skill.Strategy.Judge.Model, skill.Strategy.Judge.Enabled)

	// Panel members inherit from strategy when not set in YAML
	inherited := inheritPanelDefaults(skill)
	if inherited.Strategy.Panel[0].Provider != "local-ds" {
		t.Errorf("panel[0] provider should inherit 'local-ds', got %q", inherited.Strategy.Panel[0].Provider)
	}
	if inherited.Strategy.Panel[0].Model != "deepseek-v4-flash" {
		t.Errorf("panel[0] model should inherit 'deepseek-v4-flash', got %q", inherited.Strategy.Panel[0].Model)
	}
	// Judge should also inherit
	if inherited.Strategy.Judge.Provider != "local-ds" {
		t.Errorf("judge provider should inherit 'local-ds', got %q", inherited.Strategy.Judge.Provider)
	}
}
