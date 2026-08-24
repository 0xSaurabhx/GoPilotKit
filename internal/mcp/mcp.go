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
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
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
	Code    int         `json:"code"`
	Message string      `json:"message"`
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
	Command string        `json:"command"`
	Args    []string      `json:"args,omitempty"`
	Env     []string      `json:"env,omitempty"`
	Timeout time.Duration `json:"timeout"`
}

// Client communicates with an MCP server over stdio.
type Client struct {
	config StdioConfig
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	mu     sync.Mutex
	id     int
	ready  bool

	// Response routing: the reader goroutine sends responses here,
	// and sendRequest reads from it.
	respCh chan JSONRPCResponse
}

// NewClient creates a new MCP client for stdio transport.
func NewClient(cfg StdioConfig) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &Client{
		config: cfg,
		respCh: make(chan JSONRPCResponse, 16),
	}
}

// Connect starts the MCP server process and performs the initialize handshake.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()

	c.cmd = exec.CommandContext(ctx, c.config.Command, c.config.Args...)
	if len(c.config.Env) > 0 {
		c.cmd.Env = c.config.Env
	}

	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("mcp: creating stdin pipe: %w", err)
	}

	stdoutPipe, err := c.cmd.StdoutPipe()
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("mcp: creating stdout pipe: %w", err)
	}
	c.stdout = bufio.NewScanner(stdoutPipe)

	// Start the process.
	if err := c.cmd.Start(); err != nil {
		c.mu.Unlock()
		return fmt.Errorf("mcp: starting server: %w", err)
	}

	// Start the response reader goroutine.
	go c.readResponses()

	// Perform initialize handshake (synchronous — uses c.respCh directly).
	initResult, err := c.sendRequestInternal("initialize", map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]string{
			"name":    "gopilotkit",
			"version": "1.0.0",
		},
	})
	if err != nil {
		// Release mutex before Close to avoid deadlock.
		c.mu.Unlock()
		c.Close()
		return fmt.Errorf("mcp: initialize failed: %w", err)
	}
	_ = initResult

	// Send initialized notification.
	c.sendNotification("notifications/initialized", nil)

	c.ready = true
	c.mu.Unlock()
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

	result, err := c.sendRequestInternal("tools/list", nil)
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

	result, err := c.sendRequestInternal("tools/call", map[string]interface{}{
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
// Safe to call multiple times. Does NOT acquire c.mu — the caller
// must not hold the lock when calling Close.
func (c *Client) Close() {
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait() // Wait for process to exit so stdout reader goroutine terminates.
	}
	c.mu.Lock()
	c.ready = false
	c.mu.Unlock()
}

// IsReady returns whether the client is connected and ready.
func (c *Client) IsReady() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ready
}

// sendRequestInternal sends a JSON-RPC request and waits for the matching response.
// Must be called with c.mu held. Releases c.mu while waiting for the response
// so that the readResponses goroutine can route incoming messages.
func (c *Client) sendRequestInternal(method string, params interface{}) (json.RawMessage, error) {
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

	// Release the mutex while waiting for the response so that the
	// readResponses goroutine can route incoming messages.
	c.mu.Unlock()

	deadline := time.After(c.config.Timeout)
	for {
		select {
		case resp := <-c.respCh:
			// Check if this is our response.
			if id, ok := resp.ID.(float64); ok && int(id) == reqID {
				if resp.Error != nil {
					c.mu.Lock()
					return nil, fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
				}
				c.mu.Lock()
				return resp.Result, nil
			}
			// Not ours — discard (responses arrive in order for stdio,
			// so this shouldn't happen in practice).
		case <-deadline:
			c.mu.Lock()
			return nil, fmt.Errorf("timeout waiting for response to %s", method)
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

// readResponses reads responses from stdout and routes them to respCh.
func (c *Client) readResponses() {
	for c.stdout.Scan() {
		line := c.stdout.Text()
		var resp JSONRPCResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		c.respCh <- resp
	}
}

// --- Host (manages multiple MCP server connections) ---

// ServerConfig configures an MCP server connection.
type ServerConfig struct {
	ID        string        `json:"id"`
	Command   string        `json:"command,omitempty"`
	Args      []string      `json:"args,omitempty"`
	Transport string        `json:"transport,omitempty"` // "stdio", "sse", "grpc"
	BaseURL   string        `json:"base_url,omitempty"`   // For SSE transport
	Timeout   time.Duration `json:"timeout"`
}

// Host manages connections to multiple MCP servers.
type Host struct {
	servers    map[string]*Client
	sseClients map[string]*SSEClient
	configs    map[string]ServerConfig
	mu         sync.RWMutex
}

// NewHost creates a new MCP host.
func NewHost() *Host {
	return &Host{
		servers:    make(map[string]*Client),
		sseClients: make(map[string]*SSEClient),
		configs:    make(map[string]ServerConfig),
	}
}

// ConnectServer connects to an MCP server using the configured transport.
func (h *Host) ConnectServer(ctx context.Context, cfg ServerConfig) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	transport := cfg.Transport
	if transport == "" {
		transport = "stdio" // Default to stdio.
	}

	var client *Client

	switch transport {
	case "sse":
		// For SSE transport, we wrap the SSEClient as a regular Client
		// by connecting via a different code path.
		sseClient := NewSSEClient(SSEConfig{
			BaseURL: cfg.BaseURL,
			Timeout: cfg.Timeout,
		})
		if err := sseClient.Connect(ctx); err != nil {
			return fmt.Errorf("connecting to MCP server %q via SSE: %w", cfg.ID, err)
		}
		// Store as SSE client in a separate map.
		h.sseClients[cfg.ID] = sseClient
		h.configs[cfg.ID] = cfg
		log.Printf("mcp: connected to %q via SSE (base_url=%s)", cfg.ID, cfg.BaseURL)
		return nil

	case "grpc":
		return fmt.Errorf("connecting to MCP server %q: gRPC transport not yet available", cfg.ID)

	case "stdio", "":
		client = NewClient(StdioConfig{
			Command: cfg.Command,
			Args:    cfg.Args,
			Timeout: cfg.Timeout,
		})
		if err := client.Connect(ctx); err != nil {
			return fmt.Errorf("connecting to MCP server %q via stdio: %w", cfg.ID, err)
		}
		h.servers[cfg.ID] = client

	default:
		return fmt.Errorf("unsupported MCP transport %q for server %q", transport, cfg.ID)
	}

	h.configs[cfg.ID] = cfg
	log.Printf("mcp: connected to %q via %s", cfg.ID, transport)
	return nil
}

// HasServer returns true if a server with the given ID is connected.
func (h *Host) HasServer(serverID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, stdioOk := h.servers[serverID]
	_, sseOk := h.sseClients[serverID]
	return stdioOk || sseOk
}

// GetClient returns the stdio MCP client for a server.
func (h *Host) GetClient(serverID string) (*Client, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c, ok := h.servers[serverID]
	return c, ok
}

// GetSSEClient returns the SSE MCP client for a server.
func (h *Host) GetSSEClient(serverID string) (*SSEClient, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c, ok := h.sseClients[serverID]
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

	for _, client := range h.servers {
		client.Close()
	}
	for _, client := range h.sseClients {
		client.Close()
	}
	h.servers = make(map[string]*Client)
	h.sseClients = make(map[string]*SSEClient)
	return nil
}
