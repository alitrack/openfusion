// Package search provides web search backends for F9 context injection.
package search

import (
	"fmt"
	"strings"

	"github.com/lhy/openfusion/internal/types"
)

// Result holds a single web search result.
type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// Searcher is the interface for search backends.
type Searcher interface {
	Search(query string) ([]Result, error)
}

// FormatContext formats search results as a markdown section for prompt injection.
func FormatContext(results []Result) string {
	if len(results) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("### Web Search Context\n\n")
	b.WriteString("The following information was retrieved from web searches:\n\n")

	for _, r := range results {
		b.WriteString(fmt.Sprintf("- **%s**\n", r.Title))
		b.WriteString(fmt.Sprintf("  Source: %s\n", r.URL))
		if r.Snippet != "" {
			b.WriteString(fmt.Sprintf("  %s\n", r.Snippet))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// AddSearchContext performs web search if configured and injects results as a system message.
// Returns the original messages unchanged if search is disabled or fails.
func AddSearchContext(messages []types.ChatMessage, ws *types.WebSearchConfig) []types.ChatMessage {
	if ws == nil || !ws.Enabled {
		return messages
	}

	// Get API key from config or env
	apiKey := ws.APIKey
	if apiKey == "" {
		return messages
	}

	// Build searcher
	backend := ws.Backend
	if backend == "" {
		backend = "brave"
	}

	maxResults := ws.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}

	maxChars := ws.MaxContextLength
	if maxChars <= 0 {
		maxChars = 2000
	}

	var searcher Searcher
	switch backend {
	case "brave":
		searcher = NewBrave(BraveConfig{
			APIKey:     apiKey,
			MaxResults: maxResults,
			MaxChars:   maxChars,
		})
	default:
		// Unknown backend — skip silently
		return messages
	}

	// Extract prompt for search query
	prompt := ExtractLastUserMessage(messages)
	if prompt == "" {
		return messages
	}

	results, err := searcher.Search(prompt)
	if err != nil || len(results) == 0 {
		return messages
	}

	// Inject search context as a system message
	context := FormatContext(results)
	if context == "" {
		return messages
	}

	// Prepend as system message (after any existing system messages)
	ctxMsg := types.ChatMessage{Role: "system", Content: context}
	result := make([]types.ChatMessage, 0, len(messages)+1)
	result = append(result, ctxMsg)
	result = append(result, messages...)
	return result
}

// ExtractLastUserMessage extracts the content of the last user message.
func ExtractLastUserMessage(messages []types.ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}
