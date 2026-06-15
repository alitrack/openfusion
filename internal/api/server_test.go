package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lhy/openfusion/internal/cache"
	"github.com/lhy/openfusion/internal/types"
)

// ---------------------------------------------------------------------------
// Mock engine
// ---------------------------------------------------------------------------

type mockEngine struct {
	executeFunc func(string, *types.ChatRequest) (*types.ChatResponse, error)
	presets     []PresetSummary
}

func (m *mockEngine) Execute(preset string, req *types.ChatRequest) (*types.ChatResponse, error) {
	if m.executeFunc != nil {
		return m.executeFunc(preset, req)
	}
	return &types.ChatResponse{
		ID:     "of_test",
		Object: "chat.completion",
		Model:  preset,
		Choices: []types.Choice{
			{Index: 0, Message: types.ChatMessage{Role: "assistant", Content: "mock fusion answer"}},
		},
		Usage: types.Usage{TotalTokens: 100},
	}, nil
}

func (m *mockEngine) ExecuteAuto(req *types.ChatRequest) (*types.ChatResponse, error) {
	return m.Execute("openfusion/auto", req)
}

func (m *mockEngine) ListPresets() []PresetSummary {
	return m.presets
}

func (m *mockEngine) Metrics() interface{} {
	return nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestListModels(t *testing.T) {
	engine := &mockEngine{
		presets: []PresetSummary{
			{ID: "openfusion/budget", Object: "model", OwnedBy: "openfusion"},
			{ID: "openfusion/quality", Object: "model", OwnedBy: "openfusion"},
		},
	}
	srv := NewServer(engine, "", ":8080", nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models", nil)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatalf("body[\"data\"] is not an array")
	}
	if len(data) != 2 {
		t.Fatalf("model count = %d, want 2", len(data))
	}
}

func TestChatCompletions_HappyPath(t *testing.T) {
	engine := &mockEngine{}
	srv := NewServer(engine, "", ":8080", nil)
	payload := `{"model":"openfusion/budget","messages":[{"role":"user","content":"hello"}]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. Body: %s", rec.Code, rec.Body.String())
	}

	var resp types.ChatResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "mock fusion answer" {
		t.Errorf("content = %q, want %q", resp.Choices[0].Message.Content, "mock fusion answer")
	}
}

func TestChatCompletions_EmptyMessages(t *testing.T) {
	engine := &mockEngine{}
	srv := NewServer(engine, "", ":8080", nil)
	payload := `{"model":"test","messages":[]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestChatCompletions_NoModel(t *testing.T) {
	engine := &mockEngine{}
	srv := NewServer(engine, "", ":8080", nil)
	payload := `{"messages":[{"role":"user","content":"hi"}]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestChatCompletions_PanelOverride_InvalidMember(t *testing.T) {
	engine := &mockEngine{}
	srv := NewServer(engine, "", ":8080", nil)
	// Missing model in panel member
	payload := `{"model":"test","messages":[{"role":"user","content":"hi"}],"panel":[{"provider":"p1"}]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestChatCompletions_JudgeOverride_Invalid(t *testing.T) {
	engine := &mockEngine{}
	srv := NewServer(engine, "", ":8080", nil)
	// Missing provider in judge override
	payload := `{"model":"test","messages":[{"role":"user","content":"hi"}],"judge":{"model":"gpt4"}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestChatCompletions_ValidPanelOverride(t *testing.T) {
	engine := &mockEngine{}
	srv := NewServer(engine, "", ":8080", nil)
	payload := `{"model":"test","messages":[{"role":"user","content":"hi"}],"panel":[{"provider":"p1","model":"m1"},{"provider":"p2","model":"m2"}],"judge":{"provider":"p1","model":"judge-model"}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("valid override got status %d, want 200. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestChatCompletions_JudgeOverrideOnly(t *testing.T) {
	engine := &mockEngine{}
	srv := NewServer(engine, "", ":8080", nil)
	payload := `{"model":"test","messages":[{"role":"user","content":"hi"}],"judge":{"provider":"p1","model":"judge-model"}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("judge-only override got status %d, want 200. Body: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Cache key tests
// ---------------------------------------------------------------------------

func TestCacheKeyWithOverrides(t *testing.T) {
	msgs := []types.ChatMessage{{Role: "user", Content: "hello"}}

	// Same messages, different overrides → different keys
	key1 := cache.Key("test", msgs, nil, nil)
	key2 := cache.Key("test", msgs, []types.PanelMember{{Provider: "a", Model: "b"}}, nil)
	key3 := cache.Key("test", msgs, nil, &types.JudgeConfig{Provider: "c", Model: "d"})

	if key1 == key2 {
		t.Error("cache key should differ with panel override")
	}
	if key1 == key3 {
		t.Error("cache key should differ with judge override")
	}
	if len(key1) == 0 || len(key2) == 0 || len(key3) == 0 {
		t.Error("cache key should be non-empty")
	}
}

func TestAuthMiddleware(t *testing.T) {
	engine := &mockEngine{}
	srv := NewServer(engine, "secret123", ":8080", nil)
	t.Run("no auth header", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/v1/models", nil)
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("wrong key", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/v1/models", nil)
		req.Header.Set("Authorization", "Bearer wrong-key")
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("correct key", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/v1/models", nil)
		req.Header.Set("Authorization", "Bearer secret123")
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})
}
