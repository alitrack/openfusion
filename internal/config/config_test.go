package config

import (
	"os"
	"testing"
)

func TestExpandEnv(t *testing.T) {
	os.Setenv("OF_TEST_KEY", "sk-test123")
	defer os.Unsetenv("OF_TEST_KEY")

	tests := []struct {
		input    string
		expected string
	}{
		{"${OF_TEST_KEY}", "sk-test123"},
		{"${NONEXISTENT}", "${NONEXISTENT}"},
		{"${NONEXISTENT:default}", "default"},
		{"prefix_${OF_TEST_KEY}_suffix", "prefix_sk-test123_suffix"},
		{"no_var_here", "no_var_here"},
	}

	for _, tt := range tests {
		result := expandEnv(tt.input)
		if result != tt.expected {
			t.Errorf("expandEnv(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestLoadConfig(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "openfusion-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	content := `
server:
  addr: ":9090"
  auth_token: "test-token"

providers:
  openrouter:
    base_url: "https://openrouter.ai/api/v1"
    api_key: "${OF_TEST_KEY}"

fusion:
  default_timeout: 60
  panel_timeout_per_model: 30
`
	if _, err := tmpFile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	os.Setenv("OF_TEST_KEY", "sk-test456")
	defer os.Unsetenv("OF_TEST_KEY")

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Addr != ":9090" {
		t.Errorf("Server.Addr = %q, want %q", cfg.Server.Addr, ":9090")
	}
	if cfg.Server.AuthToken != "test-token" {
		t.Errorf("Server.AuthToken = %q, want %q", cfg.Server.AuthToken, "test-token")
	}
	if cfg.Providers["openrouter"].APIKey != "sk-test456" {
		t.Errorf("OpenRouter APIKey = %q, want %q", cfg.Providers["openrouter"].APIKey, "sk-test456")
	}
	if cfg.Fusion.DefaultTimeout != 60 {
		t.Errorf("DefaultTimeout = %d, want %d", cfg.Fusion.DefaultTimeout, 60)
	}
}
