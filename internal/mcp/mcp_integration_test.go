package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// buildMockServer compiles the mock MCP server and returns the path to the binary.
func buildMockServer(t *testing.T) string {
	t.Helper()

	binary := filepath.Join(t.TempDir(), "mock_mcp_server")

	// Find the testdata source.
	src := filepath.Join("testdata", "mock_mcp_server.go")

	cmd := exec.Command("go", "build", "-o", binary, src)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to build mock MCP server: %v", err)
	}

	return binary
}

func TestMCPClientConnect(t *testing.T) {
	binary := buildMockServer(t)

	client := NewClient(StdioConfig{
		Command: binary,
		Timeout: 10 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	if !client.IsReady() {
		t.Error("expected client to be ready after Connect")
	}
}

func TestMCPClientListTools(t *testing.T) {
	binary := buildMockServer(t)

	client := NewClient(StdioConfig{
		Command: binary,
		Timeout: 10 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name] = true
	}
	if !names["echo"] {
		t.Error("expected 'echo' tool")
	}
	if !names["add"] {
		t.Error("expected 'add' tool")
	}
}

func TestMCPClientCallEchoTool(t *testing.T) {
	binary := buildMockServer(t)

	client := NewClient(StdioConfig{
		Command: binary,
		Timeout: 10 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	args, _ := json.Marshal(map[string]string{"text": "hello world"})
	result, err := client.CallTool(ctx, "echo", args)
	if err != nil {
		t.Fatalf("CallTool(echo) failed: %v", err)
	}

	if result.IsError {
		t.Error("expected no error in result")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}
	if result.Content[0].Text != "hello world" {
		t.Errorf("expected 'hello world', got %q", result.Content[0].Text)
	}
}

func TestMCPClientCallAddTool(t *testing.T) {
	binary := buildMockServer(t)

	client := NewClient(StdioConfig{
		Command: binary,
		Timeout: 10 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	args, _ := json.Marshal(map[string]float64{"a": 3, "b": 7})
	result, err := client.CallTool(ctx, "add", args)
	if err != nil {
		t.Fatalf("CallTool(add) failed: %v", err)
	}

	if result.IsError {
		t.Error("expected no error in result")
	}
	if result.Content[0].Text != "10" {
		t.Errorf("expected '10', got %q", result.Content[0].Text)
	}
}

func TestMCPClientCallUnknownTool(t *testing.T) {
	binary := buildMockServer(t)

	client := NewClient(StdioConfig{
		Command: binary,
		Timeout: 10 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	args, _ := json.Marshal(map[string]string{})
	_, err := client.CallTool(ctx, "nonexistent", args)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestMCPClientSequentialCalls(t *testing.T) {
	binary := buildMockServer(t)

	client := NewClient(StdioConfig{
		Command: binary,
		Timeout: 10 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	// Make multiple sequential calls to verify response routing works.
	for i := 0; i < 5; i++ {
		args, _ := json.Marshal(map[string]string{"text": "test"})
		result, err := client.CallTool(ctx, "echo", args)
		if err != nil {
			t.Fatalf("CallTool(echo) iteration %d failed: %v", i, err)
		}
		if result.Content[0].Text != "test" {
			t.Errorf("iteration %d: expected 'test', got %q", i, result.Content[0].Text)
		}
	}
}

func TestMCPHostConnectAndList(t *testing.T) {
	binary := buildMockServer(t)

	host := NewHost()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := host.ConnectServer(ctx, ServerConfig{
		ID:      "test-server",
		Command: binary,
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("ConnectServer failed: %v", err)
	}
	defer host.CloseAll()

	if !host.HasServer("test-server") {
		t.Error("expected HasServer to return true")
	}
	if host.HasServer("nonexistent") {
		t.Error("expected HasServer to return false for nonexistent")
	}

	servers := host.ListServers()
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}

	client, ok := host.GetClient("test-server")
	if !ok {
		t.Fatal("expected to get client")
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
}

func TestMCPClientConnectFailure(t *testing.T) {
	client := NewClient(StdioConfig{
		Command: "/nonexistent/binary",
		Timeout: 2 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	if err == nil {
		t.Fatal("expected error for nonexistent binary")
	}
}

func TestMCPClientListToolsNotConnected(t *testing.T) {
	client := NewClient(StdioConfig{
		Command: "echo",
		Timeout: 2 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := client.ListTools(ctx)
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}
