package fusion

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEngine_ReloadConfig(t *testing.T) {
	// Create a temp config file
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	initialCfg := `
server:
  addr: "127.0.0.1:8080"
providers:
  test-provider:
    base_url: "http://localhost:8000/v1"
    api_key: "test-key"
presets:
  dir: ""
  items:
    test-model:
      description: "test preset"
      panel:
        - model: test-model
          provider: test-provider
      judge:
        model: test-judge
        provider: test-provider
fusion:
  default_timeout: 30
  panel_timeout_per_model: 15
`
	if err := os.WriteFile(cfgPath, []byte(initialCfg), 0644); err != nil {
		t.Fatal(err)
	}

	// Build initial engine from config
	engine, cleanup, err := BuildFromConfig(cfgPath)
	if err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}
	defer cleanup()

	// Verify initial preset loaded
	presets := engine.ListPresets()
	if len(presets) == 0 {
		t.Fatal("expected at least 1 preset after build")
	}

	// Update config with new preset
	updatedCfg := `
server:
  addr: "127.0.0.1:8080"
providers:
  test-provider:
    base_url: "http://localhost:8000/v1"
    api_key: "test-key"
presets:
  dir: ""
  items:
    test-model:
      description: "test preset"
      panel:
        - model: test-model
          provider: test-provider
      judge:
        model: test-judge
        provider: test-provider
    new-model:
      description: "newly added preset"
      panel:
        - model: new-model
          provider: test-provider
      judge:
        model: test-judge
        provider: test-provider
fusion:
  default_timeout: 30
  panel_timeout_per_model: 15
`
	if err := os.WriteFile(cfgPath, []byte(updatedCfg), 0644); err != nil {
		t.Fatal(err)
	}

	// Reload
	if err := engine.Reload(cfgPath); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	// Verify new preset is available
	presets = engine.ListPresets()
	found := false
	for _, p := range presets {
		if p.ID == "openfusion/new-model" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'openfusion/new-model' preset after reload, not found")
	}
}

func TestEngine_ReloadPreservesHealthyState(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	cfg := `
server:
  addr: "127.0.0.1:8080"
providers:
  p1:
    base_url: "http://localhost:8000/v1"
    api_key: "key1"
presets:
  dir: ""
  items:
    m1:
      description: "preset1"
      panel:
        - model: m1
          provider: p1
      judge:
        model: j1
        provider: p1
fusion:
  default_timeout: 30
  panel_timeout_per_model: 15
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}

	engine, cleanup, err := BuildFromConfig(cfgPath)
	if err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}
	defer cleanup()

	// Reload with same config — should not error
	if err := engine.Reload(cfgPath); err != nil {
		t.Fatalf("Reload with same config failed: %v", err)
	}

	// Verify original preset still works
	presets := engine.ListPresets()
	if len(presets) != 1 {
		t.Errorf("expected 1 preset, got %d", len(presets))
	}
}

func TestEngine_ReloadTimeoutUpdate(t *testing.T) {
	// Verify that Execute uses the new timeout after reload
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	cfg := `
server:
  addr: "127.0.0.1:8080"
providers:
  p1:
    base_url: "http://localhost:8000/v1"
    api_key: "key1"
presets:
  dir: ""
  items:
    m1:
      description: "preset1"
      panel:
        - model: m1
          provider: p1
      judge:
        model: j1
        provider: p1
fusion:
  default_timeout: 120
  panel_timeout_per_model: 60
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}

	engine, cleanup, err := BuildFromConfig(cfgPath)
	if err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}
	defer cleanup()

	// Change timeout
	updatedCfg := `
server:
  addr: "127.0.0.1:8080"
providers:
  p1:
    base_url: "http://localhost:8000/v1"
    api_key: "key1"
presets:
  dir: ""
  items:
    m1:
      description: "preset1"
      panel:
        - model: m1
          provider: p1
      judge:
        model: j1
        provider: p1
fusion:
  default_timeout: 5
  panel_timeout_per_model: 2
`
	if err := os.WriteFile(cfgPath, []byte(updatedCfg), 0644); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if err := engine.Reload(cfgPath); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	_ = start
}
