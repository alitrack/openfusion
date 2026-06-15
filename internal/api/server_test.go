package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	srv := NewServer(engine, "", ":8080")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models", nil)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&body)
	data := body["data"].([]interface{})
	if len(data) != 2 {
		t.Fatalf("model count = %d, want 2", len(data))
	}
}

func TestChatCompletions_HappyPath(t *testing.T) {
	engine := &mockEngine{}
	srv := NewServer(engine, "", ":8080")

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
	srv := NewServer(engine, "", ":8080")

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
	srv := NewServer(engine, "", ":8080")

	payload := `{"messages":[{"role":"user","content":"hi"}]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestAuthMiddleware(t *testing.T) {
	engine := &mockEngine{}
	srv := NewServer(engine, "secret123", ":8080")

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
