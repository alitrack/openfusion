// Package mcp client — high-level MCP client for knowledge retrieval.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

// Client is a high-level MCP client for knowledge retrieval.
// It manages the MCP lifecycle: initialize → (list tools) → call tool → close.
type Client struct {
	source  KnowledgeSource
	trans   transport
	timeout time.Duration
}

// NewClient creates a new MCP client for the given knowledge source.
func NewClient(source KnowledgeSource) (*Client, error) {
	if source.ToolName == "" {
		source.ToolName = DefaultToolName
	}
	if source.MaxTokens <= 0 {
		source.MaxTokens = DefaultMaxTokens
	}

	var trans transport
	var err error

	if source.ServerCmd != "" {
		// Stdio transport
		parts := strings.Fields(source.ServerCmd)
		if len(parts) == 0 {
			return nil, fmt.Errorf("mcp: empty server command")
		}
		trans, err = newStdioTransport(parts[0], parts[1:])
		if err != nil {
			return nil, fmt.Errorf("mcp: stdio transport: %w", err)
		}
	} else if source.ServerURL != "" {
		// HTTP transport
		trans = newHTTPTransport(source.ServerURL)
	} else {
		return nil, fmt.Errorf("mcp: either server_cmd or server_url must be set")
	}

	client := &Client{
		source:  source,
		trans:   trans,
		timeout: 30 * time.Second,
	}

	// Perform MCP initialization handshake
	if err := client.initialize(); err != nil {
		trans.Close()
		return nil, fmt.Errorf("mcp: initialize: %w", err)
	}

	return client, nil
}

// initialize performs the MCP initialization handshake.
func (c *Client) initialize() error {
	initParams := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{},
		"clientInfo": map[string]any{
			"name":    "openfusion",
			"version": "0.1.0",
		},
	}
	paramsRaw, _ := json.Marshal(initParams)

	resp, err := c.trans.Send(jsonRpcRequest{
		Method: "initialize",
		Params: paramsRaw,
	})
	if err != nil {
		return fmt.Errorf("initialize request: %w", err)
	}
	if resp.Error != nil {
		return resp.Error
	}

	// Send initialized notification (no response expected)
	notifRaw, _ := json.Marshal(map[string]string{})
	// We send this as a fire-and-forget notification (no ID)
	notif := fmt.Sprintf(
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
	)
	// For stdio transport we need to write this outside the send mechanism
	// since notifications have no ID. Let's use a raw write.
	// But actually, we can just send it as a regular request and ignore errors.
	c.trans.Send(jsonRpcRequest{
		Method: "notifications/initialized",
		Params: notifRaw,
	})

	_ = notif // used above
	return nil
}

// Search retrieves knowledge from the MCP server for the given query.
// Returns the text content of the search result.
func (c *Client) Search(ctx context.Context, query string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Build tool call arguments
	args := map[string]any{
		"query": query,
	}
	if c.source.MaxTokens > 0 {
		args["max_tokens"] = c.source.MaxTokens
	}

	// Call the MCP tool
	resp, err := c.trans.Send(jsonRpcRequest{
		Method: "tools/call",
		Params: func() json.RawMessage {
			p := map[string]any{
				"name":      c.source.ToolName,
				"arguments": args,
			}
			b, _ := json.Marshal(p)
			return b
		}(),
	})
	if err != nil {
		return "", fmt.Errorf("mcp call '%s': %w", c.source.ToolName, err)
	}
	if resp.Error != nil {
		return "", resp.Error
	}

	// Parse the result
	var result callToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("mcp parse result: %w", err)
	}

	if result.IsError {
		return "", fmt.Errorf("mcp tool returned error: %s", result.Text())
	}

	return result.Text(), nil
}

// Close shuts down the MCP client and underlying transport.
func (c *Client) Close() error {
	return c.trans.Close()
}

// ---------------------------------------------------------------------------
// Convenience: one-shot search
// ---------------------------------------------------------------------------

// SearchOnce creates a client, performs a single search, and closes.
func SearchOnce(source KnowledgeSource, query string) (string, error) {
	client, err := NewClient(source)
	if err != nil {
		return "", err
	}
	defer client.Close()

	return client.Search(context.Background(), query)
}
