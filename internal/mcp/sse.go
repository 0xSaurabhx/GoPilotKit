package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// --- JSON-RPC types (reused from mcp.go) ---

// SSEConfig configures an SSE-based MCP server connection.
type SSEConfig struct {
	BaseURL string        `json:"base_url"` // e.g. "http://localhost:3000"
	Timeout time.Duration `json:"timeout"`
}

// SSEClient communicates with a remote MCP server over SSE transport.
//
// Protocol:
//  1. Connect to GET /sse to receive the SSE stream
//  2. Server sends "event: endpoint\ndata: <path>\n\n" telling us where to POST
//  3. Client sends JSON-RPC requests via POST to that endpoint
//  4. Server returns 202 Accepted and sends the response asynchronously via SSE
//  5. Client reads "event: message\ndata: <json-rpc-response>\n\n"
type SSEClient struct {
	config   SSEConfig
	endpoint string // The POST endpoint from the server
	mu       sync.Mutex
	id       int
	ready    bool
	client   *http.Client // For POST requests (with timeout)

	// SSE stream state
	sseCtx    context.Context
	sseCancel context.CancelFunc
	respCh    chan JSONRPCResponse
	epCh      chan struct{} // Signals that endpoint has been received
}

// NewSSEClient creates a new MCP client for SSE transport.
func NewSSEClient(cfg SSEConfig) *SSEClient {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &SSEClient{
		config: cfg,
		client: &http.Client{Timeout: cfg.Timeout},
		respCh: make(chan JSONRPCResponse, 64),
		epCh:   make(chan struct{}, 1),
	}
}

// Connect establishes the SSE stream and performs the MCP initialize handshake.
func (c *SSEClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ready {
		return nil
	}

	// Start the SSE listener in the background.
	c.sseCtx, c.sseCancel = context.WithCancel(ctx)
	go c.listenSSE(c.sseCtx)

	// Wait for the endpoint event from the SSE stream.
	endpointTimeout := c.config.Timeout
	if endpointTimeout > 30*time.Second {
		endpointTimeout = 30 * time.Second
	}
	if endpointTimeout < 5*time.Second {
		endpointTimeout = 5 * time.Second
	}

	// Release the lock while waiting so listenSSE can set the endpoint.
	c.mu.Unlock()
	select {
	case <-c.epCh:
		// Endpoint received — re-acquire lock for the rest.
		c.mu.Lock()
	case <-time.After(endpointTimeout):
		c.mu.Lock()
		c.sseCancel()
		return fmt.Errorf("mcp/sse: timeout waiting for endpoint event")
	case <-c.sseCtx.Done():
		c.mu.Lock()
		return fmt.Errorf("mcp/sse: context cancelled while waiting for endpoint")
	}

	// Perform initialize handshake.
	initResult, err := c.sendRequest("initialize", map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]string{
			"name":    "gopilotkit-sse",
			"version": "1.0.0",
		},
	})
	if err != nil {
		c.sseCancel()
		return fmt.Errorf("mcp/sse: initialize failed: %w", err)
	}
	_ = initResult

	// Send initialized notification.
	c.sendNotification("notifications/initialized", nil)

	c.ready = true
	log.Printf("mcp/sse: connected to %s (endpoint=%s)", c.config.BaseURL, c.endpoint)
	return nil
}

// ListTools returns the tools exposed by the remote MCP server.
func (c *SSEClient) ListTools(ctx context.Context) ([]Tool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.ready {
		return nil, fmt.Errorf("mcp/sse: not connected")
	}

	result, err := c.sendRequestLocked("tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("mcp/sse: listing tools: %w", err)
	}

	var toolsResp struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(result, &toolsResp); err != nil {
		return nil, fmt.Errorf("mcp/sse: parsing tools list: %w", err)
	}

	return toolsResp.Tools, nil
}

// CallTool invokes a tool on the remote MCP server.
func (c *SSEClient) CallTool(ctx context.Context, name string, args json.RawMessage) (*ToolResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.ready {
		return nil, fmt.Errorf("mcp/sse: not connected")
	}

	result, err := c.sendRequestLocked("tools/call", map[string]interface{}{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return nil, fmt.Errorf("mcp/sse: calling tool %q: %w", name, err)
	}

	var toolResult ToolResult
	if err := json.Unmarshal(result, &toolResult); err != nil {
		return nil, fmt.Errorf("mcp/sse: parsing tool result: %w", err)
	}

	return &toolResult, nil
}

// Close shuts down the SSE connection.
func (c *SSEClient) Close() {
	if c.sseCancel != nil {
		c.sseCancel()
	}
	c.mu.Lock()
	c.ready = false
	c.mu.Unlock()
}

// IsReady returns whether the client is connected and ready.
func (c *SSEClient) IsReady() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ready
}

// listenSSE connects to the SSE endpoint and routes incoming events.
// It does NOT acquire c.mu — it only writes to epCh and respCh which are safe.
func (c *SSEClient) listenSSE(ctx context.Context) {
	sseURL := strings.TrimSuffix(c.config.BaseURL, "/") + "/sse"

	req, err := http.NewRequestWithContext(ctx, "GET", sseURL, nil)
	if err != nil {
		log.Printf("mcp/sse: creating SSE request: %v", err)
		return
	}
	req.Header.Set("Accept", "text/event-stream")

	// Use a transport without a body-read timeout for the SSE stream.
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	}
	noTimeoutClient := &http.Client{Transport: transport}

	resp, err := noTimeoutClient.Do(req)
	if err != nil {
		log.Printf("mcp/sse: SSE connection failed: %v", err)
		return
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var eventType string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			switch eventType {
			case "endpoint":
				// Store endpoint WITHOUT acquiring c.mu (we're in a goroutine).
				// Only the listener reads/writes endpoint before Connect returns,
				// and after Connect returns we only read it.
				c.endpoint = data
				log.Printf("mcp/sse: received endpoint %s", data)
				// Signal that the endpoint is ready.
				select {
				case c.epCh <- struct{}{}:
				default:
				}

			case "message":
				// A JSON-RPC response.
				var resp JSONRPCResponse
				if err := json.Unmarshal([]byte(data), &resp); err != nil {
					continue
				}
				select {
				case c.respCh <- resp:
				default:
					log.Printf("mcp/sse: response buffer full, dropping response")
				}

			default:
				// Unknown event type — try parsing as a message anyway.
				var resp JSONRPCResponse
				if err := json.Unmarshal([]byte(data), &resp); err == nil && resp.JSONRPC == "2.0" {
					select {
					case c.respCh <- resp:
					default:
					}
				}
			}

			eventType = "" // Reset for the next event.
		}
	}
}

// sendRequest is used during Connect (c.mu is held). Posts and waits for response.
func (c *SSEClient) sendRequest(method string, params interface{}) (json.RawMessage, error) {
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

	// POST the request to the endpoint.
	endpointURL := strings.TrimSuffix(c.config.BaseURL, "/") + c.endpoint
	httpResp, err := c.client.Post(endpointURL, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", endpointURL, err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK && httpResp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("POST %s returned %d: %s", endpointURL, httpResp.StatusCode, body)
	}

	// Read the response from the SSE channel.
	deadline := time.After(c.config.Timeout)
	for {
		select {
		case resp := <-c.respCh:
			if id, ok := resp.ID.(float64); ok && int(id) == reqID {
				if resp.Error != nil {
					return nil, fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
				}
				return resp.Result, nil
			}
		case <-deadline:
			return nil, fmt.Errorf("timeout waiting for response to %s", method)
		}
	}
}

// sendRequestLocked is used after Connect. Releases c.mu while waiting.
func (c *SSEClient) sendRequestLocked(method string, params interface{}) (json.RawMessage, error) {
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

	endpointURL := strings.TrimSuffix(c.config.BaseURL, "/") + c.endpoint

	// Release mutex while doing I/O.
	c.mu.Unlock()

	httpResp, err := c.client.Post(endpointURL, "application/json", bytes.NewReader(data))
	if err != nil {
		c.mu.Lock()
		return nil, fmt.Errorf("POST %s: %w", endpointURL, err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK && httpResp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(httpResp.Body)
		c.mu.Lock()
		return nil, fmt.Errorf("POST %s returned %d: %s", endpointURL, httpResp.StatusCode, body)
	}

	// Read the response from the SSE channel.
	deadline := time.After(c.config.Timeout)
	for {
		select {
		case resp := <-c.respCh:
			if id, ok := resp.ID.(float64); ok && int(id) == reqID {
				c.mu.Lock()
				if resp.Error != nil {
					return nil, fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
				}
				return resp.Result, nil
			}
		case <-deadline:
			c.mu.Lock()
			return nil, fmt.Errorf("timeout waiting for response to %s", method)
		}
	}
}

// sendNotification sends a JSON-RPC notification (no response expected).
func (c *SSEClient) sendNotification(method string, params interface{}) {
	notif := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		notif["params"] = params
	}
	data, _ := json.Marshal(notif)

	endpointURL := strings.TrimSuffix(c.config.BaseURL, "/") + c.endpoint
	c.client.Post(endpointURL, "application/json", bytes.NewReader(data))
}
