package preset

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lhy/openfusion/internal/types"
)

func TestRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	p := &types.Preset{
		Name:        "test-preset",
		Description: "A test preset",
		Panel: []types.PanelMember{
			{Provider: "openai", Model: "gpt-4o"},
		},
		Judge: types.JudgeConfig{
			Provider: "openai",
			Model:    "gpt-4o",
		},
	}

	if err := r.Register(p); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	got, ok := r.Get("test-preset")
	if !ok {
		t.Fatal("Get() returned not found")
	}
	if got.Description != "A test preset" {
		t.Errorf("Description = %q, want %q", got.Description, "A test preset")
	}

	// Test "openfusion/" prefix stripping
	got2, ok := r.Get("openfusion/test-preset")
	if !ok {
		t.Fatal("Get('openfusion/test-preset') returned not found")
	}
	if got2.Name != "test-preset" {
		t.Errorf("Name = %q, want %q", got2.Name, "test-preset")
	}
}

func TestRegisterDuplicate(t *testing.T) {
	r := NewRegistry()
	p := &types.Preset{Name: "dup", Panel: []types.PanelMember{{Model: "a"}}, Judge: types.JudgeConfig{Model: "b"}}
	r.Register(p)
	err := r.Register(p)
	if err == nil {
		t.Fatal("expected error on duplicate register")
	}
}

func TestLoadDir(t *testing.T) {
	dir, err := os.MkdirTemp("", "openfusion-presets-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	presetContent := `
name: budget
description: Budget preset
panel:
  - provider: openrouter
    model: gemini-3-flash
    system: "You are a helpful assistant."
judge:
  provider: openrouter
  model: claude-opus
`
	if err := os.WriteFile(filepath.Join(dir, "budget.yaml"), []byte(presetContent), 0644); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry()
	if err := r.LoadDir(dir); err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}

	p, ok := r.Get("budget")
	if !ok {
		t.Fatal("budget preset not found after LoadDir")
	}
	if len(p.Panel) != 1 {
		t.Errorf("Panel len = %d, want 1", len(p.Panel))
	}
	if p.Judge.Model != "claude-opus" {
		t.Errorf("Judge.Model = %q, want %q", p.Judge.Model, "claude-opus")
	}
}

func TestLoadInvalidFile(t *testing.T) {
	dir, err := os.MkdirTemp("", "openfusion-invalid-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Empty panel
	presetContent := `name: empty
panel: []
judge:
  model: test`
	if err := os.WriteFile(filepath.Join(dir, "empty.yaml"), []byte(presetContent), 0644); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry()
	err = r.LoadDir(dir)
	if err == nil {
		t.Fatal("expected error for empty panel")
	}
}
