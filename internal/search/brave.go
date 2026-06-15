package search

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BraveConfig controls the Brave Search API backend.
type BraveConfig struct {
	APIKey     string // Brave Search API key (required)
	MaxResults int    // Max results to return (default 5)
	MaxChars   int    // Max characters per snippet (default 2000)
}

// Brave is a search backend using the Brave Search API.
// Free tier: 2,000 queries/month, sign up at https://api.search.brave.com/
type Brave struct {
	client *http.Client
	config BraveConfig
	apiURL string
}

// NewBrave creates a Brave Search backend.
func NewBrave(cfg BraveConfig) *Brave {
	if cfg.MaxResults <= 0 {
		cfg.MaxResults = 5
	}
	if cfg.MaxChars <= 0 {
		cfg.MaxChars = 2000
	}
	return &Brave{
		client: &http.Client{Timeout: 15 * time.Second},
		config: cfg,
		apiURL: "https://api.search.brave.com/res/v1/web/search",
	}
}

// braveResponse represents the Brave Search API response.
type braveResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

// Search performs a web search via the Brave Search API.
func (b *Brave) Search(query string) ([]Result, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}

	if b.config.APIKey == "" {
		return nil, fmt.Errorf("Brave Search: API key required (get one free at https://api.search.brave.com/)")
	}

	reqURL := fmt.Sprintf("%s?q=%s&count=%d", b.apiURL, url.QueryEscape(query), b.config.MaxResults)
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("brave: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("X-Subscription-Token", b.config.APIKey)

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("brave: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var braveResp braveResponse
	if err := json.NewDecoder(resp.Body).Decode(&braveResp); err != nil {
		return nil, fmt.Errorf("brave: decode response: %w", err)
	}

	results := make([]Result, 0, len(braveResp.Web.Results))
	for _, r := range braveResp.Web.Results {
		snippet := r.Description
		if b.config.MaxChars > 0 && len(snippet) > b.config.MaxChars {
			snippet = snippet[:b.config.MaxChars]
		}
		results = append(results, Result{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: snippet,
		})
	}

	return results, nil
}
