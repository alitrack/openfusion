// Package mcp transport — handles stdio and HTTP transport for MCP protocol.
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Transport interface
// ---------------------------------------------------------------------------

// transport abstracts the MCP message transport layer.
type transport interface {
	// Send sends a JSON-RPC request and returns the response.
	Send(req jsonRpcRequest) (jsonRpcResponse, error)
	// Close shuts down the transport.
	Close() error
}

// ---------------------------------------------------------------------------
// Stdio transport — spawn subprocess, communicate via stdin/stdout
// ---------------------------------------------------------------------------

type stdioTransport struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	reader  *bufio.Reader
	mu      sync.Mutex
	nextID  int64
	closed  bool
}

func newStdioTransport(command string, args []string) (*stdioTransport, error) {
	cmd := exec.Command(command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: create stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: create stdout pipe: %w", err)
	}
	// Stderr goes to our stderr for debugging
	cmd.Stderr = nil // inherit parent stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp stdio: start process: %w", err)
	}

	t := &stdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		reader: bufio.NewReader(stdout),
		nextID: 1,
	}

	return t, nil
}

// Send writes a JSON-RPC request to stdin and reads the response from stdout.
// Uses Content-Length framing per MCP spec.
func (t *stdioTransport) Send(req jsonRpcRequest) (jsonRpcResponse, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return jsonRpcResponse{}, fmt.Errorf("mcp stdio: transport closed")
	}

	// Use the transport's sequential ID if not set
	if req.ID == 0 {
		req.ID = t.nextID
		t.nextID++
	}

	body, err := json.Marshal(req)
	if err != nil {
		return jsonRpcResponse{}, fmt.Errorf("mcp stdio: marshal request: %w", err)
	}

	// Write Content-Length framed message
	header := fmt.Sprintf("Content-Length: %d\r\nContent-Type: application/json\r\n\r\n", len(body))
	if _, err := t.stdin.Write([]byte(header)); err != nil {
		return jsonRpcResponse{}, fmt.Errorf("mcp stdio: write header: %w", err)
	}
	if _, err := t.stdin.Write(body); err != nil {
		return jsonRpcResponse{}, fmt.Errorf("mcp stdio: write body: %w", err)
	}

	// Read Content-Length header
	contentLength, err := t.readContentLength()
	if err != nil {
		return jsonRpcResponse{}, fmt.Errorf("mcp stdio: read header: %w", err)
	}

	// Read exactly contentLength bytes
	respBody := make([]byte, contentLength)
	if _, err := io.ReadFull(t.reader, respBody); err != nil {
		return jsonRpcResponse{}, fmt.Errorf("mcp stdio: read response: %w", err)
	}

	var resp jsonRpcResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return jsonRpcResponse{}, fmt.Errorf("mcp stdio: unmarshal response: %w", err)
	}

	return resp, nil
}

func (t *stdioTransport) readContentLength() (int, error) {
	for {
		line, err := t.reader.ReadString('\n')
		if err != nil {
			return 0, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			// End of headers
			continue
		}
		if strings.HasPrefix(line, "Content-Length:") {
			n, err := strconv.Atoi(strings.TrimSpace(line[15:]))
			if err != nil {
				return 0, fmt.Errorf("parse Content-Length: %w", err)
			}
			// Read the blank line after Content-Type
			t.reader.ReadString('\n')
			return n, nil
		}
	}
}

func (t *stdioTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true

	// Close stdin to signal EOF to the subprocess
	if t.stdin != nil {
		t.stdin.Close()
	}

	// Wait with timeout
	done := make(chan error, 1)
	go func() {
		done <- t.cmd.Wait()
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.cmd.Process.Kill()
	}
	return nil
}

// ---------------------------------------------------------------------------
// HTTP/SSE transport — POST to a URL endpoint
// ---------------------------------------------------------------------------

type httpTransport struct {
	baseURL string
	client  *http.Client
	nextID  int64
	mu      sync.Mutex
}

func newHTTPTransport(baseURL string) *httpTransport {
	return &httpTransport{
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		nextID: 1,
	}
}

func (t *httpTransport) Send(req jsonRpcRequest) (jsonRpcResponse, error) {
	t.mu.Lock()
	if req.ID == 0 {
		req.ID = t.nextID
		t.nextID++
	}
	t.mu.Unlock()

	body, err := json.Marshal(req)
	if err != nil {
		return jsonRpcResponse{}, fmt.Errorf("mcp http: marshal: %w", err)
	}

	httpReq, err := http.NewRequest("POST", t.baseURL, bytes.NewReader(body))
	if err != nil {
		return jsonRpcResponse{}, fmt.Errorf("mcp http: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := t.client.Do(httpReq)
	if err != nil {
		return jsonRpcResponse{}, fmt.Errorf("mcp http: do request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return jsonRpcResponse{}, fmt.Errorf("mcp http: read response: %w", err)
	}

	var resp jsonRpcResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return jsonRpcResponse{}, fmt.Errorf("mcp http: unmarshal: %w", err)
	}

	return resp, nil
}

func (t *httpTransport) Close() error {
	t.client.CloseIdleConnections()
	return nil
}
