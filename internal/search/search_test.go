package search

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrave_EmptyQuery(t *testing.T) {
	b := NewBrave(BraveConfig{APIKey: "test", MaxResults: 3, MaxChars: 2000})
	results, err := b.Search("")
	if err != nil {
		t.Fatalf("empty query should not error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("empty query: got %d results, want 0", len(results))
	}
}

func TestBrave_NoAPIKey(t *testing.T) {
	b := NewBrave(BraveConfig{APIKey: "", MaxResults: 3})
	_, err := b.Search("golang")
	if err == nil || !contains(err.Error(), "API key required") {
		t.Errorf("expected API key error, got: %v", err)
	}
}

func TestBrave_ParsesResponse(t *testing.T) {
	// Mock Brave Search API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		resp := braveResponse{}
		resp.Web.Results = []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		}{
			{Title: "Go Programming", URL: "https://go.dev/", Description: "The Go programming language website"},
			{Title: "Go Wiki", URL: "https://github.com/golang/go/wiki", Description: "Unofficial Go wiki"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	b := &Brave{
		client:  server.Client(),
		config:  BraveConfig{APIKey: "test-key", MaxResults: 5, MaxChars: 2000},
		apiURL:  server.URL,
	}
	results, err := b.Search("golang")
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Title != "Go Programming" || results[0].URL != "https://go.dev/" {
		t.Errorf("result[0] = %+v", results[0])
	}
	if results[0].Snippet != "The Go programming language website" {
		t.Errorf("snippet = %q", results[0].Snippet)
	}
}

func TestFormatContext(t *testing.T) {
	results := []Result{
		{Title: "Go Lang", URL: "https://go.dev/", Snippet: "Official site"},
	}
	ctx := FormatContext(results)
	if !contains(ctx, "Go Lang") || !contains(ctx, "https://go.dev/") {
		t.Errorf("FormatContext missing expected content: %q", ctx)
	}
}

func TestFormatContextEmpty(t *testing.T) {
	ctx := FormatContext(nil)
	if ctx != "" {
		t.Errorf("expected empty string, got %q", ctx)
	}
}

func TestBrave_APIFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"reason":"rate limited"}`))
	}))
	defer server.Close()

	b := &Brave{
		client: server.Client(),
		config: BraveConfig{APIKey: "test", MaxResults: 5, MaxChars: 2000},
		apiURL: server.URL,
	}
	_, err := b.Search("golang")
	if err == nil {
		t.Error("expected error for 429 status")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
