package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockMCPSSEServer simulates an MCP server over SSE transport.
// The server returns 202 for POST requests and sends responses via the SSE stream.
type mockMCPSSEServer struct {
	mu       sync.Mutex
	sseConns []chan JSONRPCResponse // SSE connections to push responses to
}

func (s *mockMCPSSEServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	// Create a channel for this SSE connection to receive responses.
	respCh := make(chan JSONRPCResponse, 64)
	s.mu.Lock()
	s.sseConns = append(s.sseConns, respCh)
	s.mu.Unlock()

	// Send the endpoint event.
	fmt.Fprintf(w, "event: endpoint\ndata: /mcp\n\n")
	flusher.Flush()

	// Forward responses from the channel as SSE events.
	for {
		select {
		case <-r.Context().Done():
			return
		case resp := <-respCh:
			data, _ := json.Marshal(resp)
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
			flusher.Flush()
		case <-time.After(1 * time.Second):
			// Heartbeat.
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func (s *mockMCPSSEServer) handlePost(w http.ResponseWriter, r *http.Request) {
	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Build the response.
	var resp JSONRPCResponse
	resp.JSONRPC = "2.0"
	resp.ID = req.ID

	switch req.Method {
	case "initialize":
		resp.Result, _ = json.Marshal(map[string]interface{}{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]interface{}{},
			"serverInfo":      map[string]string{"name": "mock-sse", "version": "1.0.0"},
		})
	case "tools/list":
		resp.Result, _ = json.Marshal(map[string]interface{}{
			"tools": []Tool{
				{Name: "echo", Description: "Echoes input", InputSchema: json.RawMessage(`{"type":"object"}`)},
			},
		})
	case "tools/call":
		resp.Result, _ = json.Marshal(ToolResult{
			Content: []ToolContent{{Type: "text", Text: "Echo: test"}},
		})
	case "notifications/initialized":
		// Notification — no response needed.
		w.WriteHeader(http.StatusAccepted)
		return
	default:
		resp.Error = &JSONRPCError{Code: -32601, Message: "method not found"}
	}

	// Push the response to all SSE connections.
	s.mu.Lock()
	for _, ch := range s.sseConns {
		select {
		case ch <- resp:
		default:
			// Channel full — skip.
		}
	}
	s.mu.Unlock()

	// Return 202 Accepted — the response is delivered via SSE.
	w.WriteHeader(http.StatusAccepted)
}

func startMockMCPSSEServer(t *testing.T) *httptest.Server {
	t.Helper()
	mock := &mockMCPSSEServer{}

	mux := http.NewServeMux()
	mux.HandleFunc("/sse", mock.handleSSE)
	mux.HandleFunc("/mcp", mock.handlePost)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestSSEClientConnect(t *testing.T) {
	server := startMockMCPSSEServer(t)

	client := NewSSEClient(SSEConfig{
		BaseURL: server.URL,
		Timeout: 15 * time.Second,
	})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if !client.IsReady() {
		t.Fatal("expected client to be ready after Connect")
	}
}

func TestSSEClientListTools(t *testing.T) {
	server := startMockMCPSSEServer(t)

	client := NewSSEClient(SSEConfig{
		BaseURL: server.URL,
		Timeout: 15 * time.Second,
	})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "echo" {
		t.Errorf("expected tool name 'echo', got %q", tools[0].Name)
	}
}

func TestSSEClientCallTool(t *testing.T) {
	server := startMockMCPSSEServer(t)

	client := NewSSEClient(SSEConfig{
		BaseURL: server.URL,
		Timeout: 15 * time.Second,
	})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	args, _ := json.Marshal(map[string]string{"message": "hello"})
	result, err := client.CallTool(ctx, "echo", args)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if result.IsError {
		t.Fatal("expected no error in result")
	}
	if len(result.Content) != 1 || result.Content[0].Text != "Echo: test" {
		t.Errorf("expected 'Echo: test', got %v", result.Content)
	}
}

func TestSSEClientClose(t *testing.T) {
	server := startMockMCPSSEServer(t)

	client := NewSSEClient(SSEConfig{
		BaseURL: server.URL,
		Timeout: 15 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	client.Close()

	if client.IsReady() {
		t.Fatal("expected client to not be ready after Close")
	}
}

func TestHostSSETransport(t *testing.T) {
	server := startMockMCPSSEServer(t)

	host := NewHost()
	defer host.CloseAll()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := host.ConnectServer(ctx, ServerConfig{
		ID:        "remote-server",
		Transport: "sse",
		BaseURL:   server.URL,
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("ConnectServer: %v", err)
	}

	if !host.HasServer("remote-server") {
		t.Fatal("expected host to have the SSE server")
	}

	sseClient, ok := host.GetSSEClient("remote-server")
	if !ok {
		t.Fatal("expected SSE client to be available")
	}
	if !sseClient.IsReady() {
		t.Fatal("expected SSE client to be ready")
	}
}

func TestSSEClientConnectTimeout(t *testing.T) {
	// Server that never sends the endpoint event.
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher := w.(http.Flusher)
		for {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(1 * time.Second):
				fmt.Fprintf(w, ": heartbeat\n\n")
				flusher.Flush()
			}
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewSSEClient(SSEConfig{
		BaseURL: server.URL,
		Timeout: 2 * time.Second,
	})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	if err == nil {
		t.Fatal("expected error for timeout waiting for endpoint")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}
