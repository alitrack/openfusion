// Package mcp search — inject MCP knowledge retrieval results into chat messages.
package mcp

import (
	"fmt"
	"log"
	"strings"

	"github.com/lhy/openfusion/internal/types"
)

// ---------------------------------------------------------------------------
// SearchAndInject
// ---------------------------------------------------------------------------

// SearchAndInject queries all configured MCP knowledge sources for the given
// prompt and injects the results as context messages into the request.
// Returns the modified message list, or the original if no sources are configured.
func SearchAndInject(messages []types.ChatMessage, cfg *KnowledgeConfig) []types.ChatMessage {
	if cfg == nil || len(cfg.Sources) == 0 {
		return messages
	}

	prompt := types.ExtractLastUserMessage(messages)
	if prompt == "" {
		return messages
	}

	var results []SearchResult

	for _, source := range cfg.Sources {
		content, err := SearchOnce(source, prompt)
		if err != nil {
			log.Printf("[mcp] search error on %q: %v", source.ToolName, err)
			results = append(results, SearchResult{
				Source: source.ToolName,
				Error:  err.Error(),
			})
			continue
		}
		if content == "" {
			continue
		}
		results = append(results, SearchResult{
			Source:  source.ToolName,
			Content: content,
		})
	}

	if len(results) == 0 {
		return messages
	}

	// Build knowledge context block
	context := formatKnowledgeContext(results)
	if context == "" {
		return messages
	}

	// Prepend as a system message (after any existing system messages)
	ctxMsg := types.ChatMessage{
		Role:    "system",
		Content: context,
	}
	result := make([]types.ChatMessage, 0, len(messages)+1)
	result = append(result, ctxMsg)
	result = append(result, messages...)

	return result
}

// ---------------------------------------------------------------------------
// Formatting
// ---------------------------------------------------------------------------

// formatKnowledgeContext formats search results as a markdown section.
func formatKnowledgeContext(results []SearchResult) string {
	if len(results) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("### Knowledge Base Context\n\n")
	b.WriteString("The following information was retrieved from domain knowledge sources:\n\n")

	for _, r := range results {
		if r.Error != "" {
			b.WriteString(fmt.Sprintf("> **Source: %s** — [error: %s]\n\n", r.Source, r.Error))
			continue
		}
		b.WriteString(fmt.Sprintf("#### From: %s\n\n", r.Source))
		b.WriteString(r.Content)
		b.WriteString("\n\n")
	}

	return b.String()
}
