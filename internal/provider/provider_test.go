package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lhy/openfusion/internal/types"
)

func TestOpenAIAdapter_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"id": "cmpl-test",
			"object": "chat.completion",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "Hello from test"}
			}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		}`))
	}))
	defer srv.Close()

	adapter := NewOpenAIAdapter("test", srv.URL, "test-key")
	resp, err := adapter.ChatCompletion(context.Background(), &types.ChatRequest{
		Model: "gpt-4o",
		Messages: []types.ChatMessage{
			{Role: "user", Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("Choices len = %d, want 1", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "Hello from test" {
		t.Errorf("Content = %q, want %q", resp.Choices[0].Message.Content, "Hello from test")
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", resp.Usage.TotalTokens)
	}
}

func TestOpenAIAdapter_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "unauthorized"}`))
	}))
	defer srv.Close()

	adapter := NewOpenAIAdapter("test", srv.URL, "wrong-key")
	_, err := adapter.ChatCompletion(context.Background(), &types.ChatRequest{
		Model:    "gpt-4o",
		Messages: []types.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for unauthorized")
	}
}

func TestManager(t *testing.T) {
	m := NewManager()
	adapter := NewOpenAIAdapter("openai", "http://test", "key")
	m.Register("openai", adapter)

	got, err := m.Get("openai")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Name() != "openai" {
		t.Errorf("Name() = %q, want %q", got.Name(), "openai")
	}

	_, err = m.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}
