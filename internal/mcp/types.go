// Package mcp implements a lightweight MCP (Model Context Protocol) client
// for knowledge retrieval and injection into the fusion pipeline.
//
// MCP-Know architecture: OpenFusion (Layer 2) uses MCP client to retrieve
// domain knowledge from external knowledge services (Layer 3) before
// dispatching to panel models.
package mcp

import (
	"encoding/json"
	"fmt"
)

// ---------------------------------------------------------------------------
// JSON-RPC 2.0 types
// ---------------------------------------------------------------------------

// jsonRpcRequest is a JSON-RPC 2.0 request.
type jsonRpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// jsonRpcResponse is a JSON-RPC 2.0 response.
type jsonRpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRpcError   `json:"error,omitempty"`
}

// jsonRpcError represents a JSON-RPC error.
type jsonRpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *jsonRpcError) Error() string {
	return fmt.Sprintf("MCP error %d: %s", e.Code, e.Message)
}

// ---------------------------------------------------------------------------
// Initialize types
// ---------------------------------------------------------------------------

type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    capabilities   `json:"capabilities"`
	ServerInfo      serverInfo     `json:"serverInfo"`
}

type capabilities struct {
	Tools     *toolCapabilities     `json:"tools,omitempty"`
	Resources *resourceCapabilities `json:"resources,omitempty"`
}

type toolCapabilities struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type resourceCapabilities struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ---------------------------------------------------------------------------
// Tool types
// ---------------------------------------------------------------------------

// ToolDefinition describes an MCP tool.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// toolListResult is the response from tools/list.
type toolListResult struct {
	Tools []ToolDefinition `json:"tools"`
}

// ---------------------------------------------------------------------------
// Call tool types
// ---------------------------------------------------------------------------

// callToolResult is the response from tools/call.
type callToolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// toolContent represents a content item in a tool call result.
type toolContent struct {
	Type string `json:"type"` // "text", "image", "resource"
	Text string `json:"text,omitempty"`
	// Image and resource fields omitted for simplicity; we only handle text.
}

// Text returns the text content of the first text block, or empty string.
func (r *callToolResult) Text() string {
	for _, c := range r.Content {
		if c.Type == "text" {
			return c.Text
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// MCP Client config
// ---------------------------------------------------------------------------

// KnowledgeSource defines a single MCP knowledge source.
type KnowledgeSource struct {
	// ServerURL is the HTTP endpoint for SSE transport.
	// Mutually exclusive with ServerCmd.
	ServerURL string `yaml:"server_url,omitempty" json:"server_url,omitempty"`

	// ServerCmd is the subprocess command for stdio transport.
	// Format: "python /path/to/server.py" or "node /path/to/server.js"
	// Mutually exclusive with ServerURL.
	ServerCmd string `yaml:"server_cmd,omitempty" json:"server_cmd,omitempty"`

	// ToolName is the MCP tool to call for knowledge retrieval.
	// Default: "search_knowledge"
	ToolName string `yaml:"tool_name,omitempty" json:"tool_name,omitempty"`

	// MaxTokens limits the number of characters in the retrieved context.
	// Default: 4000
	MaxTokens int `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`
}

// KnowledgeConfig holds the complete MCP knowledge configuration.
type KnowledgeConfig struct {
	// Sources is the list of MCP knowledge sources to query.
	Sources []KnowledgeSource `yaml:"sources,omitempty" json:"sources,omitempty"`
}

// ---------------------------------------------------------------------------
// Result types
// ---------------------------------------------------------------------------

// SearchResult holds the result from a single MCP knowledge source.
type SearchResult struct {
	Source  string `json:"source"`
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

const (
	// DefaultToolName is the default MCP tool name for knowledge retrieval.
	DefaultToolName = "search_knowledge"
	// DefaultMaxTokens is the default context length limit.
	DefaultMaxTokens = 4000
)
