package search

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lhy/openfusion/internal/types"
)

func TestAddSearchContext_Disabled(t *testing.T) {
	msgs := []types.ChatMessage{{Role: "user", Content: "hello"}}
	result := AddSearchContext(msgs, nil)
	if len(result) != 1 {
		t.Errorf("nil config: got %d messages, want 1", len(result))
	}

	disabled := &types.WebSearchConfig{Enabled: false}
	result = AddSearchContext(msgs, disabled)
	if len(result) != 1 {
		t.Errorf("disabled: got %d messages, want 1", len(result))
	}
}

func TestAddSearchContext_NoAPIKey(t *testing.T) {
	msgs := []types.ChatMessage{{Role: "user", Content: "hello"}}
	cfg := &types.WebSearchConfig{Enabled: true, APIKey: ""}
	result := AddSearchContext(msgs, cfg)
	if len(result) != 1 {
		t.Errorf("no api key: got %d messages, want 1", len(result))
	}
}

func TestAddSearchContext_EmptyUserMessage(t *testing.T) {
	msgs := []types.ChatMessage{{Role: "system", Content: "be helpful"}}
	cfg := &types.WebSearchConfig{Enabled: true, APIKey: "test-key"}
	result := AddSearchContext(msgs, cfg)
	if len(result) != 1 {
		t.Errorf("empty user: got %d messages, want 1", len(result))
	}
}

func TestAddSearchContext_RejectsEmptyBackend(t *testing.T) {
	msgs := []types.ChatMessage{{Role: "user", Content: "hello"}}
	cfg := &types.WebSearchConfig{
		Enabled:  true,
		APIKey:   "test",
		Backend:  "unknown-backend",
	}
	result := AddSearchContext(msgs, cfg)
	if len(result) != 1 {
		t.Errorf("unknown backend: got %d messages, want 1 (no injection)", len(result))
	}
}

func TestAddSearchContext_DefaultBackendIsBrave(t *testing.T) {
	// When Backend is empty, AddSearchContext defaults to "brave"
	// Since we can't create a live Brave request in unit test, we verify
	// the function doesn't crash and returns gracefully
	msgs := []types.ChatMessage{{Role: "user", Content: "hello"}}
	cfg := &types.WebSearchConfig{
		Enabled: true,
		APIKey:  "test-key",
		Backend: "",
	}
	result := AddSearchContext(msgs, cfg)
	// Should attempt brave search, which will fail with bad API key → return original
	if len(result) != 1 {
		t.Errorf("default brave: got %d messages, want 1 (graceful)", len(result))
	}
}

func TestAddSearchContext_WithMockBrave(t *testing.T) {
	// Mock Brave API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("X-Subscription-Token")
		if auth != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		resp := braveResponse{}
		resp.Web.Results = []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		}{
			{Title: "AI News", URL: "https://example.com/ai", Description: "Latest AI developments"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Construct Brave manually with mock server URL
	brave := &Brave{
		client:  server.Client(),
		config:  BraveConfig{APIKey: "test-key", MaxResults: 3, MaxChars: 2000},
		apiURL: server.URL,
	}

	// Test Brave search directly
	results, err := brave.Search("AI developments")
	if err != nil {
		t.Fatalf("brave.Search() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("brave.Search() returned %d results", len(results))
	}
	if results[0].Title != "AI News" {
		t.Errorf("title = %q, want %q", results[0].Title, "AI News")
	}

	// Test FormatContext
	ctx := FormatContext(results)
	if !containsStr(ctx, "AI News") || !containsStr(ctx, "https://example.com/ai") {
		t.Errorf("FormatContext missing content: %q", ctx)
	}
}
