package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/0xsaurabhx/GoPilotKit/internal/config"
	"github.com/0xsaurabhx/GoPilotKit/internal/protocol/agui"
	"github.com/0xsaurabhx/GoPilotKit/internal/runtime"
)

// --- Mock LLM Server ---

// mockLLMResponse simulates an OpenAI-compatible /v1/chat/completions endpoint.
type mockLLMResponse struct {
	mu           sync.Mutex
	responses    []mockResponse
	responseIdx  int
}

type mockResponse struct {
	Content   string
	ToolCalls []mockToolCall
}

type mockToolCall struct {
	ID   string
	Name string
	Args string
}

func (m *mockLLMResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/chat/completions" && r.Method == "POST" {
		m.mu.Lock()
		idx := m.responseIdx
		if idx < len(m.responses) {
			m.responseIdx++
		}
		m.mu.Unlock()

		if idx >= len(m.responses) {
			idx = len(m.responses) - 1
		}

		resp := m.responses[idx]
		m.writeChatResponse(w, resp)
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}

func (m *mockLLMResponse) writeChatResponse(w http.ResponseWriter, resp mockResponse) {
	// Check if streaming is requested by parsing the request body.
	// For streaming, we send SSE events. For non-streaming, we send JSON.
	// The engine always uses streaming, so we send SSE.

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher := w.(http.Flusher)

	// Send content deltas.
	if resp.Content != "" {
		for i := 0; i < len(resp.Content); i += 10 {
			end := i + 10
			if end > len(resp.Content) {
				end = len(resp.Content)
			}
			chunk := map[string]interface{}{
				"id":    "mock-chatcmpl-123",
				"model": "mock-model",
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"delta": map[string]string{"content": resp.Content[i:end]},
					},
				},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}

	// Send tool calls if any.
	if len(resp.ToolCalls) > 0 {
		toolCalls := make([]map[string]interface{}, len(resp.ToolCalls))
		for i, tc := range resp.ToolCalls {
			toolCalls[i] = map[string]interface{}{
				"id":   tc.ID,
				"type": "function",
				"function": map[string]interface{}{
					"name":      tc.Name,
					"arguments": tc.Args,
				},
			}
			chunk := map[string]interface{}{
				"id":    "mock-chatcmpl-123",
				"model": "mock-model",
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"delta": map[string]interface{}{"tool_calls": toolCalls},
					},
				},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}

	// Send finish chunk.
	finishReason := "stop"
	if len(resp.ToolCalls) > 0 {
		finishReason = "tool_calls"
	}
	finishChunk := map[string]interface{}{
		"id":    "mock-chatcmpl-123",
		"model": "mock-model",
		"choices": []map[string]interface{}{
			{
				"index":          0,
				"delta":          map[string]string{},
				"finish_reason":  finishReason,
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     10,
			"completion_tokens": 20,
			"total_tokens":      30,
		},
	}
	data, _ := json.Marshal(finishChunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()

	// Send [DONE].
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// --- Build MCP test server ---

func buildMockMCPServer(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "mock_mcp.go")
	binaryPath := filepath.Join(tmpDir, "mock_mcp_server")

	// Copy the mock MCP server source.
	src, _ := os.ReadFile("../../internal/mcp/testdata/mock_mcp_server.go")
	if err := os.WriteFile(srcPath, src, 0644); err != nil {
		t.Fatalf("writing mock MCP source: %v", err)
	}

	cmd := exec.Command("go", "build", "-o", binaryPath, srcPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building mock MCP server: %v\n%s", err, out)
	}
	return binaryPath
}

// --- Integration Tests ---

func TestEndToEndSimpleRun(t *testing.T) {
	// Start mock LLM server.
	llmServer := &mockLLMResponse{
		responses: []mockResponse{
			{Content: "Hello! I can help you with that. The weather today is sunny with a high of 75°F."},
		},
	}
	llm := httptest.NewServer(llmServer)
	defer llm.Close()

	// Create a temp SQLite database.
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create config.
	cfg := &config.IntelligenceConfig{
		Version: "v1alpha",
		Profile: config.ProfilePersonal,
		Server: config.ServerConfig{
			Port:              0,
			Transport:         []string{"sse"},
			CORSAllowedOrigins: []string{"*"},
		},
		Storage: config.StorageConfig{
			Driver:        "sqlite",
			ConnectionURI: dbPath,
			VectorStore: config.VectorStoreConfig{
				Driver:    "embedded",
				Dimension: 1536,
			},
		},
		Providers: []config.ProviderConfig{
			{
				ID:      "mock-llm",
				Type:    "openai",
				Model:   "mock-model",
				APIKey:  "mock-key",
				BaseURL: llm.URL + "/v1",
			},
		},
		Agents: []config.AgentConfig{
			{
				Name:         "test-agent",
				Provider:     "mock-llm",
				SystemPrompt: "You are a helpful assistant.",
				Tools:        []string{},
			},
		},
	}

	// Create engine.
	engine, err := runtime.NewEngine(cfg)
	if err != nil {
		t.Fatalf("creating engine: %v", err)
	}
	defer engine.Close()

	// Create handler.
	handler := agui.NewHandler(engine.HandleRun)

	// Start HTTP server.
	server := httptest.NewServer(handler)
	defer server.Close()

	// Send a run request.
	reqBody, _ := json.Marshal(map[string]interface{}{
		"agent":    "test-agent",
		"message":  "What's the weather like?",
		"thread_id": "test-thread-1",
	})

	resp, err := http.Post(server.URL+"/run", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST /run: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /run returned %d: %s", resp.StatusCode, body)
	}

	// Parse SSE response.
	events := parseSSEResponse(t, resp.Body)

	// Verify we got the expected events.
	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}

	eventTypes := extractEventTypes(events)
	t.Logf("Events received: %v", eventTypes)

	// Should have RUN_START, TEXT_START, TEXT_CHUNK(s), TEXT_END, RUN_END.
	if !contains(eventTypes, "RUN_START") {
		t.Error("missing RUN_START event")
	}
	if !contains(eventTypes, "RUN_END") {
		t.Error("missing RUN_END event")
	}

	// Check that the text content is in the events.
	textChunks := extractTextChunks(events)
	fullText := strings.Join(textChunks, "")
	if !strings.Contains(fullText, "sunny") {
		t.Errorf("expected response to contain 'sunny', got: %q", fullText)
	}
}

func TestEndToEndHealthCheck(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Start a minimal mock LLM.
	llmServer := &mockLLMResponse{responses: []mockResponse{{Content: "ok"}}}
	llm := httptest.NewServer(llmServer)
	defer llm.Close()

	cfg := &config.IntelligenceConfig{
		Version: "v1alpha",
		Profile: config.ProfilePersonal,
		Server: config.ServerConfig{
			Port:              0,
			Transport:         []string{"sse"},
			CORSAllowedOrigins: []string{"*"},
		},
		Storage: config.StorageConfig{
			Driver:        "sqlite",
			ConnectionURI: dbPath,
			VectorStore:   config.VectorStoreConfig{Driver: "embedded", Dimension: 1536},
		},
		Providers: []config.ProviderConfig{
			{ID: "mock", Type: "openai", Model: "m", APIKey: "k", BaseURL: llm.URL + "/v1"},
		},
		Agents: []config.AgentConfig{
			{Name: "agent", Provider: "mock", SystemPrompt: "You are helpful."},
		},
	}

	engine, err := runtime.NewEngine(cfg)
	if err != nil {
		t.Fatalf("creating engine: %v", err)
	}
	defer engine.Close()

	handler := agui.NewHandler(engine.HandleRun)
	server := httptest.NewServer(handler)
	defer server.Close()

	// Test /health endpoint.
	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var health map[string]string
	json.NewDecoder(resp.Body).Decode(&health)
	if health["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", health["status"])
	}
}

func TestEndToEndInvalidRequest(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	llmServer := &mockLLMResponse{responses: []mockResponse{{Content: "ok"}}}
	llm := httptest.NewServer(llmServer)
	defer llm.Close()

	cfg := &config.IntelligenceConfig{
		Version: "v1alpha",
		Profile: config.ProfilePersonal,
		Server: config.ServerConfig{
			Port:              0,
			Transport:         []string{"sse"},
			CORSAllowedOrigins: []string{"*"},
		},
		Storage: config.StorageConfig{
			Driver:        "sqlite",
			ConnectionURI: dbPath,
			VectorStore:   config.VectorStoreConfig{Driver: "embedded", Dimension: 1536},
		},
		Providers: []config.ProviderConfig{
			{ID: "mock", Type: "openai", Model: "m", APIKey: "k", BaseURL: llm.URL + "/v1"},
		},
		Agents: []config.AgentConfig{
			{Name: "agent", Provider: "mock", SystemPrompt: "You are helpful."},
		},
	}

	engine, err := runtime.NewEngine(cfg)
	if err != nil {
		t.Fatalf("creating engine: %v", err)
	}
	defer engine.Close()

	handler := agui.NewHandler(engine.HandleRun)
	server := httptest.NewServer(handler)
	defer server.Close()

	// Missing agent field.
	resp, _ := http.Post(server.URL+"/run", "application/json", bytes.NewReader([]byte(`{"message":"hi"}`)))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for missing agent, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Missing message field.
	resp, _ = http.Post(server.URL+"/run", "application/json", bytes.NewReader([]byte(`{"agent":"agent"}`)))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for missing message, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Unknown agent.
	resp, _ = http.Post(server.URL+"/run", "application/json", bytes.NewReader([]byte(`{"agent":"nonexistent","message":"hi"}`)))
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (errors come via SSE), got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestEndToEndToolCallLoop(t *testing.T) {
	// Build the mock MCP server.
	mcpBinary := buildMockMCPServer(t)

	// Start mock LLM that requests a tool call first, then responds with text.
	llmServer := &mockLLMResponse{
		responses: []mockResponse{
			{
				Content: "",
				ToolCalls: []mockToolCall{
					{
						ID:   "call_1",
						Name: "echo",
						Args: `{"message":"hello from tool"}`,
					},
				},
			},
			{
				Content: "The tool returned: hello from tool. I used the echo tool to test the integration.",
			},
		},
	}
	llm := httptest.NewServer(llmServer)
	defer llm.Close()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := &config.IntelligenceConfig{
		Version: "v1alpha",
		Profile: config.ProfilePersonal,
		Server: config.ServerConfig{
			Port:              0,
			Transport:         []string{"sse"},
			CORSAllowedOrigins: []string{"*"},
		},
		Storage: config.StorageConfig{
			Driver:        "sqlite",
			ConnectionURI: dbPath,
			VectorStore:   config.VectorStoreConfig{Driver: "embedded", Dimension: 1536},
		},
		Providers: []config.ProviderConfig{
			{ID: "mock", Type: "openai", Model: "m", APIKey: "k", BaseURL: llm.URL + "/v1"},
		},
		Agents: []config.AgentConfig{
			{
				Name:         "test-agent",
				Provider:     "mock",
				SystemPrompt: "You are a helpful assistant.",
				Tools:        []string{"echo"},
			},
		},
		Tools: []config.ToolConfig{
			{
				ID:        "echo",
				Type:      "mcp",
				Transport: "stdio",
				Command:   mcpBinary,
			},
		},
	}

	engine, err := runtime.NewEngine(cfg)
	if err != nil {
		t.Fatalf("creating engine: %v", err)
	}
	defer engine.Close()

	handler := agui.NewHandler(engine.HandleRun)
	server := httptest.NewServer(handler)
	defer server.Close()

	// Send a run request that triggers tool calls.
	reqBody, _ := json.Marshal(map[string]interface{}{
		"agent":    "test-agent",
		"message":  "Test the echo tool",
		"thread_id": "test-thread-tools",
	})

	resp, err := http.Post(server.URL+"/run", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST /run: %v", err)
	}
	defer resp.Body.Close()

	events := parseSSEResponse(t, resp.Body)
	eventTypes := extractEventTypes(events)
	t.Logf("Events: %v", eventTypes)

	// Should have tool call events.
	if !contains(eventTypes, "TOOL_CALL_START") {
		t.Error("missing TOOL_CALL_START event")
	}
	if !contains(eventTypes, "TOOL_OUTPUT") {
		t.Error("missing TOOL_OUTPUT event")
	}
	if !contains(eventTypes, "RUN_END") {
		t.Error("missing RUN_END event")
	}
}

// --- SSE Parsing Helpers ---

type sseEvent struct {
	Type string
	Data string
}

func parseSSEResponse(t *testing.T, body io.Reader) []sseEvent {
	t.Helper()
	var events []sseEvent
	scanner := bufio.NewScanner(body)
	var currentEvent sseEvent

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			currentEvent.Type = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			currentEvent.Data = strings.TrimPrefix(line, "data: ")
			events = append(events, currentEvent)
			currentEvent = sseEvent{}
		} else if line == "" {
			// Empty line = end of event (but we already captured).
			currentEvent = sseEvent{}
		}
	}

	return events
}

func extractEventTypes(events []sseEvent) []string {
	var types []string
	for _, e := range events {
		if e.Type != "" {
			types = append(types, e.Type)
		}
	}
	return types
}

func extractTextChunks(events []sseEvent) []string {
	var chunks []string
	for _, e := range events {
		if e.Type == "TEXT_CHUNK" {
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(e.Data), &parsed); err == nil {
				if delta, ok := parsed["delta"].(string); ok {
					chunks = append(chunks, delta)
				}
			}
		}
	}
	return chunks
}

func contains(items []string, item string) bool {
	for _, i := range items {
		if i == item {
			return true
		}
	}
	return false
}

// --- Helper to build config with mock LLM ---

func buildTestConfig(llmURL, dbPath string) *config.IntelligenceConfig {
	return &config.IntelligenceConfig{
		Version: "v1alpha",
		Profile: config.ProfilePersonal,
		Server: config.ServerConfig{
			Port:              0,
			Transport:         []string{"sse"},
			CORSAllowedOrigins: []string{"*"},
		},
		Storage: config.StorageConfig{
			Driver:        "sqlite",
			ConnectionURI: dbPath,
			VectorStore:   config.VectorStoreConfig{Driver: "embedded", Dimension: 1536},
		},
		Providers: []config.ProviderConfig{
			{ID: "mock", Type: "openai", Model: "mock-model", APIKey: "mock-key", BaseURL: llmURL + "/v1"},
		},
		Agents: []config.AgentConfig{
			{Name: "test-agent", Provider: "mock", SystemPrompt: "You are a helpful assistant."},
		},
	}
}
