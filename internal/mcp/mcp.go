// Package mcp implements the Model Context Protocol (MCP) host.
//
// MCP enables agents to discover and invoke tools exposed by external servers.
// This implementation supports stdio transport (local sub-processes) and
// provides the foundation for SSE/gRPC transport.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"time"
)

// --- MCP JSON-RPC Types ---

// JSONRPCRequest is a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id"`
	Method  string        `json:"method"`
	Params  interface{}   `json:"params,omitempty"`
}

// JSONRPCResponse is a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError is a JSON-RPC 2.0 error.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// --- MCP Tool Types ---

// Tool represents an MCP tool.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ToolCall represents a call to an MCP tool.
type ToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult is the result of calling an MCP tool.
type ToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent is a content block in a tool result.
type ToolContent struct {
	Type string `json:"type"` // "text", "image", "resource"
	Text string `json:"text,omitempty"`
	URI  string `json:"uri,omitempty"`
}

// --- MCP Client (stdio transport) ---

// StdioConfig configures a stdio-based MCP server connection.
type StdioConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Env     []string `json:"env,omitempty"`
	Timeout time.Duration `json:"timeout"`
}

// Client communicates with an MCP server over stdio.
type Client struct {
	config    StdioConfig
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Scanner
	mu        sync.Mutex
	id        int
	responses map[int]*JSONRPCResponse
	ready     bool
}

// NewClient creates a new MCP client for stdio transport.
func NewClient(cfg StdioConfig) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &Client{
		config:    cfg,
		responses: make(map[int]*JSONRPCResponse),
	}
}

// Connect starts the MCP server process and performs the initialize handshake.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cmd = exec.CommandContext(ctx, c.config.Command, c.config.Args...)
	if len(c.config.Env) > 0 {
		c.cmd.Env = c.config.Env
	}

	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcp: creating stdin pipe: %w", err)
	}

	stdoutPipe, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcp: creating stdout pipe: %w", err)
	}
	c.stdout = bufio.NewScanner(stdoutPipe)

	// Start the process.
	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("mcp: starting server: %w", err)
	}

	// Start the response reader.
	go c.readResponses()

	// Perform initialize handshake.
	initResult, err := c.sendRequest("initialize", map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]string{
			"name":    "gopilotkit",
			"version": "1.0.0",
		},
	})
	if err != nil {
		c.Close()
		return fmt.Errorf("mcp: initialize failed: %w", err)
	}
	_ = initResult

	// Send initialized notification.
	c.sendNotification("notifications/initialized", nil)

	c.ready = true
	log.Printf("mcp: connected to %s", c.config.Command)
	return nil
}

// ListTools returns the tools exposed by the MCP server.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.ready {
		return nil, fmt.Errorf("mcp: not connected")
	}

	result, err := c.sendRequestLocked("tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: listing tools: %w", err)
	}

	var toolsResp struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(result, &toolsResp); err != nil {
		return nil, fmt.Errorf("mcp: parsing tools list: %w", err)
	}

	return toolsResp.Tools, nil
}

// CallTool invokes a tool on the MCP server.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (*ToolResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.ready {
		return nil, fmt.Errorf("mcp: not connected")
	}

	result, err := c.sendRequestLocked("tools/call", map[string]interface{}{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return nil, fmt.Errorf("mcp: calling tool %q: %w", name, err)
	}

	var toolResult ToolResult
	if err := json.Unmarshal(result, &toolResult); err != nil {
		return nil, fmt.Errorf("mcp: parsing tool result: %w", err)
	}

	return &toolResult, nil
}

// Close shuts down the MCP server process.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ready = false

	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}
	return nil
}

// IsReady returns whether the client is connected and ready.
func (c *Client) IsReady() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ready
}

// sendRequest sends a JSON-RPC request and waits for the response.
func (c *Client) sendRequest(method string, params interface{}) (json.RawMessage, error) {
	return c.sendRequestLocked(method, params)
}

// sendRequestLocked sends a request while the mutex is held.
func (c *Client) sendRequestLocked(method string, params interface{}) (json.RawMessage, error) {
	c.id++
	reqID := c.id

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')

	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("writing request: %w", err)
	}

	// Wait for the response with a timeout.
	deadline := time.After(c.config.Timeout)
	for {
		select {
		case <-deadline:
			return nil, fmt.Errorf("timeout waiting for response to %s", method)
		default:
			// Check if we have a response for this ID.
			// In a real implementation, this would use a condition variable.
			time.Sleep(5 * time.Millisecond)

			// Read any available data from stdout.
			if c.stdout.Scan() {
				line := c.stdout.Text()
				var resp JSONRPCResponse
				if err := json.Unmarshal([]byte(line), &resp); err != nil {
					continue
				}
				// Check if this is our response.
				if id, ok := resp.ID.(float64); ok && int(id) == reqID {
					if resp.Error != nil {
						return nil, fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
					}
					return resp.Result, nil
				}
				// Store for later matching.
				if respID, ok := resp.ID.(float64); ok {
					c.responses[int(respID)] = &resp
				}
			}
		}
	}
}

// sendNotification sends a JSON-RPC notification (no response expected).
func (c *Client) sendNotification(method string, params interface{}) {
	notif := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		notif["params"] = params
	}
	data, _ := json.Marshal(notif)
	data = append(data, '\n')
	c.stdin.Write(data)
}

// readResponses reads responses from stdout in a goroutine.
func (c *Client) readResponses() {
	for c.stdout.Scan() {
		line := c.stdout.Text()
		var resp JSONRPCResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		c.mu.Lock()
		if id, ok := resp.ID.(float64); ok {
			c.responses[int(id)] = &resp
		}
		c.mu.Unlock()
	}
}

// --- Host (manages multiple MCP server connections) ---

// ServerConfig configures an MCP server connection.
type ServerConfig struct {
	ID      string     `json:"id"`
	Command string     `json:"command"`
	Args    []string   `json:"args,omitempty"`
	Timeout time.Duration `json:"timeout"`
}

// Host manages connections to multiple MCP servers.
type Host struct {
	servers map[string]*Client
	configs map[string]ServerConfig
	mu      sync.RWMutex
}

// NewHost creates a new MCP host.
func NewHost() *Host {
	return &Host{
		servers: make(map[string]*Client),
		configs: make(map[string]ServerConfig),
	}
}

// ConnectServer connects to an MCP server.
func (h *Host) ConnectServer(ctx context.Context, cfg ServerConfig) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	client := NewClient(StdioConfig{
		Command: cfg.Command,
		Args:    cfg.Args,
		Timeout: cfg.Timeout,
	})

	if err := client.Connect(ctx); err != nil {
		return fmt.Errorf("connecting to MCP server %q: %w", cfg.ID, err)
	}

	h.servers[cfg.ID] = client
	h.configs[cfg.ID] = cfg
	return nil
}

// GetClient returns the MCP client for a server.
func (h *Host) GetClient(serverID string) (*Client, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c, ok := h.servers[serverID]
	return c, ok
}

// ListServers returns all connected server IDs.
func (h *Host) ListServers() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var ids []string
	for id := range h.servers {
		ids = append(ids, id)
	}
	return ids
}

// CloseAll closes all MCP server connections.
func (h *Host) CloseAll() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	var firstErr error
	for id, client := range h.servers {
		if err := client.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("closing server %q: %w", id, err)
		}
	}
	h.servers = make(map[string]*Client)
	return firstErr
}
