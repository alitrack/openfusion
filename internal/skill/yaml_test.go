package skill

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPanelMemberYAMLDirect(t *testing.T) {
	// Minimal YAML test for PanelMemberConfig
	yml := `
provider: local-ds
model: deepseek-v4-flash
think: true
temperature: 0.2
`
	var pm PanelMemberConfig
	if err := yaml.Unmarshal([]byte(yml), &pm); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	t.Logf("Provider=%q Model=%q Think=%v Temp=%v", pm.Provider, pm.Model, pm.Think, pm.Temperature)
	if pm.Provider != "local-ds" {
		t.Errorf("expected provider 'local-ds', got %q", pm.Provider)
	}
	if pm.Model != "deepseek-v4-flash" {
		t.Errorf("expected model 'deepseek-v4-flash', got %q", pm.Model)
	}
}

func TestStrategyYAMLDirect(t *testing.T) {
	yml := `
provider: local-ds
model: deepseek-v4-flash
panel:
  - provider: local-ds
    model: deepseek-v4-flash
    think: true
    temperature: 0.2
    system: "You are a helpful assistant."
  - provider: local-ds
    model: deepseek-v4-flash
    think: true
    temperature: 0.5
    system: "You are a creative assistant."
judge:
  enabled: true
  system: "Synthesize the best answer."
`
	var s Strategy
	if err := yaml.Unmarshal([]byte(yml), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	t.Logf("Strategy provider=%q model=%q", s.Provider, s.Model)
	t.Logf("Panel count=%d", len(s.Panel))
	for i, pm := range s.Panel {
		t.Logf("  panel[%d]: provider=%q model=%q", i, pm.Provider, pm.Model)
		if pm.Provider != "local-ds" {
			t.Errorf("panel[%d] provider = %q, want 'local-ds'", i, pm.Provider)
		}
		if pm.Model != "deepseek-v4-flash" {
			t.Errorf("panel[%d] model = %q, want 'deepseek-v4-flash'", i, pm.Model)
		}
	}
	t.Logf("Judge: enabled=%v provider=%q model=%q", s.Judge.Enabled, s.Judge.Provider, s.Judge.Model)
}
