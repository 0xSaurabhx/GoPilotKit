package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xsaurabhx/GoPilotKit/internal/config"
	"github.com/0xsaurabhx/GoPilotKit/internal/protocol/agui"
	"github.com/0xsaurabhx/GoPilotKit/internal/store/threads"
)

// --- Mock OpenAI Server ---

// mockOpenAIServer creates a test server that mimics OpenAI's chat completions API.
// It streams a fixed response back.
func mockOpenAIServer(t *testing.T, response string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}

		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}

		// Read the request body to verify it's well-formed.
		var reqBody struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}

		// Verify we got the system prompt and user message.
		if len(reqBody.Messages) < 2 {
			t.Errorf("expected at least 2 messages, got %d", len(reqBody.Messages))
		}
		if reqBody.Messages[0].Role != "system" {
			t.Errorf("expected first message to be system, got %s", reqBody.Messages[0].Role)
		}

		// Stream the response.
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer doesn't support flushing")
		}

		// Send chunks.
		chunkSize := 5
		for i := 0; i < len(response); i += chunkSize {
			end := i + chunkSize
			if end > len(response) {
				end = len(response)
			}
			delta := response[i:end]
			chunk := fmt.Sprintf(`{"id":"chatcmpl-test","choices":[{"index":0,"delta":{"content":"%s"},"finish_reason":null}],"usage":null}`,
				delta)
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}

		// Send the final chunk with usage.
		finalChunk := fmt.Sprintf(`{"id":"chatcmpl-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":%d,"total_tokens":%d}}`,
			len(response)/4+1, 10+len(response)/4+1)
		fmt.Fprintf(w, "data: %s\n\n", finalChunk)
		flusher.Flush()

		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
}

// mockOpenAIToolServer creates a test server that requests a tool call first,
// then responds with text after the tool result is provided.
func mockOpenAIToolServer(t *testing.T, toolOutput, finalResponse string) *httptest.Server {
	t.Helper()
	callCount := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer doesn't support flushing")
		}

		callCount++

		if callCount == 1 {
			// First call: request a tool call.
			chunk := `{"id":"chatcmpl-test","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_001","type":"function","function":{"name":"echo","arguments":"{\"text\":\"hello from tool\"}"}}]},"finish_reason":null}],"usage":null}`
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()

			finalChunk := `{"id":"chatcmpl-test","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`
			fmt.Fprintf(w, "data: %s\n\n", finalChunk)
			flusher.Flush()
		} else {
			// Second call: respond with text.
			for i := 0; i < len(finalResponse); i += 5 {
				end := i + 5
				if end > len(finalResponse) {
					end = len(finalResponse)
				}
				chunk := fmt.Sprintf(`{"id":"chatcmpl-test","choices":[{"index":0,"delta":{"content":"%s"},"finish_reason":null}],"usage":null}`,
					finalResponse[i:end])
				fmt.Fprintf(w, "data: %s\n\n", chunk)
				flusher.Flush()
			}
			finalChunk := fmt.Sprintf(`{"id":"chatcmpl-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":5,"total_tokens":25}}`)
			fmt.Fprintf(w, "data: %s\n\n", finalChunk)
			flusher.Flush()
		}

		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
}

// --- Helper: parse SSE events from a buffer ---

func parseSSEEvents(body string) []map[string]interface{} {
	var events []map[string]interface{}
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	return events
}

func eventType(events []map[string]interface{}, typeName string) bool {
	for _, e := range events {
		if t, ok := e["type"].(string); ok && t == typeName {
			return true
		}
	}
	return false
}

func eventCount(events []map[string]interface{}, typeName string) int {
	count := 0
	for _, e := range events {
		if t, ok := e["type"].(string); ok && t == typeName {
			count++
		}
	}
	return count
}

func eventDeltas(events []map[string]interface{}, typeName string) string {
	var result string
	for _, e := range events {
		if t, ok := e["type"].(string); ok && t == typeName {
			if d, ok := e["delta"].(string); ok {
				result += d
			}
		}
	}
	return result
}

// --- Tests ---

func TestEngineBasicRun(t *testing.T) {
	// Set up a mock OpenAI server.
	server := mockOpenAIServer(t, "Hello! How can I help you?")
	defer server.Close()

	// Create a minimal config pointing at our mock server.
	cfg := &config.IntelligenceConfig{
		Version: "v1alpha",
		Profile: config.ProfilePersonal,
		Server: config.ServerConfig{
			Port:      9999,
			Transport: []string{"sse"},
		},
		Storage: config.StorageConfig{
			Driver:        "sqlite",
			ConnectionURI: fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()),
		},
		Providers: []config.ProviderConfig{
			{
				ID:      "test-openai",
				Type:    "openai",
				Model:   "gpt-4o",
				APIKey:  "test-key",
				BaseURL: server.URL + "/v1",
			},
		},
		Agents: []config.AgentConfig{
			{
				Name:             "test-agent",
				Provider:         "test-openai",
				FallbackProvider: "",
				SystemPrompt:     "You are a test assistant.",
				Memory:           config.MemoryConfig{SlidingWindow: 10},
			},
		},
	}

	// Initialize engine.
	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer engine.Close()

	// Create a mock HTTP response and SSE encoder.
	w := httptest.NewRecorder()
	encoder, err := agui.NewSSEEncoder(w)
	if err != nil {
		t.Fatalf("failed to create SSE encoder: %v", err)
	}
	defer encoder.Close()

	stream := agui.NewRunStream("run_test", "thread_test", encoder)
	_ = stream.SendRunStart("test-agent", nil)

	// Run the engine handler.
	engine.HandleRun(agui.RunRequest{
		Agent:   "test-agent",
		Message: "Hello",
	}, stream)

	// Parse the SSE output.
	events := parseSSEEvents(w.Body.String())

	// Verify event sequence.
	if !eventType(events, "RUN_START") {
		t.Error("expected RUN_START event")
	}
	if !eventType(events, "TEXT_START") {
		t.Error("expected TEXT_START event")
	}
	if !eventType(events, "TEXT_CHUNK") {
		t.Error("expected TEXT_CHUNK event")
	}
	if !eventType(events, "TEXT_END") {
		t.Error("expected TEXT_END event")
	}
	if !eventType(events, "RUN_END") {
		t.Error("expected RUN_END event")
	}

	// Verify the text content.
	text := eventDeltas(events, "TEXT_CHUNK")
	expectedText := "Hello! How can I help you?"
	if text != expectedText {
		t.Errorf("expected text %q, got %q", expectedText, text)
	}

	// Verify token usage in RUN_END.
	for _, e := range events {
		if typ, ok := e["type"].(string); ok && typ == "RUN_END" {
			usage, ok := e["token_usage"].(map[string]interface{})
			if !ok {
				t.Error("expected token_usage in RUN_END event")
				continue
			}
			if usage["prompt_tokens"].(float64) != 10 {
				t.Errorf("expected 10 prompt tokens, got %v", usage["prompt_tokens"])
			}
		}
	}
}

func TestEngineMissingAgent(t *testing.T) {
	// Minimal config with no matching agent.
	cfg := &config.IntelligenceConfig{
		Version: "v1alpha",
		Profile: config.ProfilePersonal,
		Server:  config.ServerConfig{Port: 9999, Transport: []string{"sse"}},
		Storage: config.StorageConfig{
			Driver:        "sqlite",
			ConnectionURI: fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()),
		},
		Providers: []config.ProviderConfig{
			{ID: "p1", Type: "openai", Model: "gpt-4o", APIKey: "key"},
		},
		Agents: []config.AgentConfig{
			{Name: "real-agent", Provider: "p1", SystemPrompt: "test"},
		},
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer engine.Close()

	w := httptest.NewRecorder()
	encoder, err := agui.NewSSEEncoder(w)
	if err != nil {
		t.Fatalf("failed to create SSE encoder: %v", err)
	}
	defer encoder.Close()

	stream := agui.NewRunStream("run_test", "thread_test", encoder)
	_ = stream.SendRunStart("nonexistent-agent", nil)

	engine.HandleRun(agui.RunRequest{
		Agent:   "nonexistent-agent",
		Message: "Hello",
	}, stream)

	events := parseSSEEvents(w.Body.String())

	if !eventType(events, "RUN_ERROR") {
		t.Error("expected RUN_ERROR event")
	}

	// Verify the error message.
	for _, e := range events {
		if typ, ok := e["type"].(string); ok && typ == "RUN_ERROR" {
			errMsg, _ := e["error"].(string)
			if !strings.Contains(errMsg, "not configured") {
				t.Errorf("expected 'not configured' in error, got: %s", errMsg)
			}
		}
	}
}

func TestEngineToolCallLoop(t *testing.T) {
	// Build the real mock MCP server.
	mcpBinary := buildMockMCPServer(t)

	// Set up a mock server that requests the "echo" tool, then responds with text.
	// The mock LLM requests "echo" which is a real tool on the mock MCP server.
	server := mockOpenAIToolServer(t, `{"result":"tool output"}`, "Based on the tool result, here is my answer.")
	defer server.Close()

	cfg := &config.IntelligenceConfig{
		Version: "v1alpha",
		Profile: config.ProfilePersonal,
		Server:  config.ServerConfig{Port: 9999, Transport: []string{"sse"}},
		Storage: config.StorageConfig{
			Driver:        "sqlite",
			ConnectionURI: fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()),
		},
		Providers: []config.ProviderConfig{
			{
				ID:      "test-openai",
				Type:    "openai",
				Model:   "gpt-4o",
				APIKey:  "test-key",
				BaseURL: server.URL + "/v1",
			},
		},
		Agents: []config.AgentConfig{
			{
				Name:         "tool-agent",
				Provider:     "test-openai",
				SystemPrompt: "You are a helpful assistant with tools.",
				Memory:       config.MemoryConfig{SlidingWindow: 10},
			},
		},
		Tools: []config.ToolConfig{
			{ID: "echo", Type: "mcp", Transport: "stdio", Command: mcpBinary},
		},
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer engine.Close()

	w := httptest.NewRecorder()
	encoder, err := agui.NewSSEEncoder(w)
	if err != nil {
		t.Fatalf("failed to create SSE encoder: %v", err)
	}
	defer encoder.Close()

	stream := agui.NewRunStream("run_tool", "thread_tool", encoder)
	_ = stream.SendRunStart("tool-agent", nil)

	engine.HandleRun(agui.RunRequest{
		Agent:   "tool-agent",
		Message: "Look up something for me",
	}, stream)

	events := parseSSEEvents(w.Body.String())

	// Verify tool call events are present.
	if !eventType(events, "TOOL_CALL_START") {
		t.Error("expected TOOL_CALL_START event")
	}
	if !eventType(events, "TOOL_CALL_END") {
		t.Error("expected TOOL_CALL_END event")
	}
	if !eventType(events, "TOOL_OUTPUT") {
		t.Error("expected TOOL_OUTPUT event")
	}

	// The tool should have executed successfully via the real MCP server.
	for _, e := range events {
		if typ, ok := e["type"].(string); ok && typ == "TOOL_OUTPUT" {
			isError, _ := e["is_error"].(bool)
			if isError {
				t.Error("expected tool output is_error=false (successful MCP execution)")
			}
		}
	}

	// Verify we got a final text response after the tool round.
	if !eventType(events, "TEXT_END") {
		t.Error("expected TEXT_END event")
	}
	if !eventType(events, "RUN_END") {
		t.Error("expected RUN_END event")
	}

	// The second LLM call should have produced text.
	textChunks := eventDeltas(events, "TEXT_CHUNK")
	if len(textChunks) == 0 {
		t.Error("expected non-empty text response after tool call")
	}
}

// buildMockMCPServer compiles the mock MCP server test binary.
func buildMockMCPServer(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "mock_mcp_server")
	src := filepath.Join("..", "mcp", "testdata", "mock_mcp_server.go")
	cmd := exec.Command("go", "build", "-o", binary, src)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to build mock MCP server: %v", err)
	}
	return binary
}

func TestEngineThreadPersistence(t *testing.T) {
	// Set up mock server.
	server := mockOpenAIServer(t, "Response to your question.")
	defer server.Close()

	cfg := &config.IntelligenceConfig{
		Version: "v1alpha",
		Profile: config.ProfilePersonal,
		Server:  config.ServerConfig{Port: 9999, Transport: []string{"sse"}},
		Storage: config.StorageConfig{
			Driver:        "sqlite",
			ConnectionURI: fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()),
		},
		Providers: []config.ProviderConfig{
			{
				ID:      "test-openai",
				Type:    "openai",
				Model:   "gpt-4o",
				APIKey:  "test-key",
				BaseURL: server.URL + "/v1",
			},
		},
		Agents: []config.AgentConfig{
			{
				Name:         "persist-agent",
				Provider:     "test-openai",
				SystemPrompt: "You are a test assistant.",
				Memory:       config.MemoryConfig{SlidingWindow: 10},
			},
		},
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer engine.Close()

	// Run 1: initial message.
	w1 := httptest.NewRecorder()
	enc1, _ := agui.NewSSEEncoder(w1)
	stream1 := agui.NewRunStream("run_1", "thread_persist", enc1)
	_ = stream1.SendRunStart("persist-agent", nil)

	engine.HandleRun(agui.RunRequest{
		Agent:   "persist-agent",
		ThreadID: "thread_persist",
		Message:  "First message",
	}, stream1)

	// Run 2: follow-up message in the same thread.
	w2 := httptest.NewRecorder()
	enc2, _ := agui.NewSSEEncoder(w2)
	stream2 := agui.NewRunStream("run_2", "thread_persist", enc2)
	_ = stream2.SendRunStart("persist-agent", nil)

	engine.HandleRun(agui.RunRequest{
		Agent:   "persist-agent",
		ThreadID: "thread_persist",
		Message:  "Follow-up message",
	}, stream2)

	// Verify the thread was created and messages persisted.
	thread, err := engine.store.GetThread("thread_persist")
	if err != nil {
		t.Fatalf("failed to get thread: %v", err)
	}
	if thread.Agent != "persist-agent" {
		t.Errorf("expected agent 'persist-agent', got '%s'", thread.Agent)
	}

	messages, err := engine.store.GetMessages("thread_persist", 100)
	if err != nil {
		t.Fatalf("failed to get messages: %v", err)
	}

	// Should have: user1, assistant1, user2, assistant2 = 4 messages.
	if len(messages) != 4 {
		t.Errorf("expected 4 messages in thread, got %d", len(messages))
	}

	// Verify user messages.
	if messages[0].Role != "user" || messages[0].Content != "First message" {
		t.Errorf("expected first user message, got: %s/%s", messages[0].Role, messages[0].Content)
	}
	if messages[2].Role != "user" || messages[2].Content != "Follow-up message" {
		t.Errorf("expected second user message, got: %s/%s", messages[2].Role, messages[2].Content)
	}
}

func TestEngineCancellation(t *testing.T) {
	// Server that takes 200ms to respond — enough time for the goroutine
	// to cancel the stream before or during the LLM call.
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		// Wait before sending the first chunk so the cancellation fires.
		select {
		case <-done:
		case <-time.After(300 * time.Millisecond):
		}

		chunk := `{"id":"chatcmpl-test","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`
		fmt.Fprintf(w, "data: %s\n\n", chunk)
		flusher.Flush()

		finalChunk := `{"id":"chatcmpl-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}}`
		fmt.Fprintf(w, "data: %s\n\n", finalChunk)
		flusher.Flush()

		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()
	defer close(done)

	cfg := &config.IntelligenceConfig{
		Version: "v1alpha",
		Profile: config.ProfilePersonal,
		Server:  config.ServerConfig{Port: 9999, Transport: []string{"sse"}},
		Storage: config.StorageConfig{
			Driver:        "sqlite",
			ConnectionURI: fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()),
		},
		Providers: []config.ProviderConfig{
			{
				ID:      "test-openai",
				Type:    "openai",
				Model:   "gpt-4o",
				APIKey:  "test-key",
				BaseURL: server.URL + "/v1",
			},
		},
		Agents: []config.AgentConfig{
			{
				Name:         "cancel-agent",
				Provider:     "test-openai",
				SystemPrompt: "You are a test assistant.",
			},
		},
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer engine.Close()

	w := httptest.NewRecorder()
	encoder, _ := agui.NewSSEEncoder(w)
	stream := agui.NewRunStream("run_cancel", "thread_cancel", encoder)
	_ = stream.SendRunStart("cancel-agent", nil)

	// Cancel the stream in a goroutine, giving the engine time to start.
	go func() {
		time.Sleep(50 * time.Millisecond)
		stream.Cancel()
	}()

	engine.HandleRun(agui.RunRequest{
		Agent:   "cancel-agent",
		Message: "Hello",
	}, stream)

	// The run should have ended — either with a RUN_END (cancelled check
	// fires before the LLM call completes) or RUN_ERROR. If the LLM
	// responded before cancellation, we still get RUN_END.
	events := parseSSEEvents(w.Body.String())
	if !eventType(events, "RUN_ERROR") && !eventType(events, "RUN_END") {
		t.Error("expected RUN_ERROR or RUN_END after cancellation")
	}
}

func TestToolExecutorHasTool(t *testing.T) {
	te := NewToolExecutor([]config.ToolConfig{
		{ID: "tool-a", Type: "mcp", Transport: "stdio", Command: "echo"},
		{ID: "tool-b", Type: "wasm", BinaryPath: "/path/to/binary.wasm"},
	}, nil)

	if !te.HasTool("tool-a") {
		t.Error("expected tool-a to exist")
	}
	if !te.HasTool("tool-b") {
		t.Error("expected tool-b to exist")
	}
	if te.HasTool("tool-c") {
		t.Error("expected tool-c to not exist")
	}

	names := te.ListTools()
	if len(names) != 2 {
		t.Errorf("expected 2 tools, got %d", len(names))
	}
}

func TestToolExecutorExecuteUnknownTool(t *testing.T) {
	te := NewToolExecutor([]config.ToolConfig{}, nil)
	_, err := te.Execute(context.Background(), "nonexistent", "{}")
	if err == nil {
		t.Error("expected error for unknown tool")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestToolExecutorExecuteStub(t *testing.T) {
	// No MCP host configured — should fail with clear error.
	te := NewToolExecutor([]config.ToolConfig{
		{ID: "my-tool", Type: "mcp", Transport: "stdio", Command: "echo"},
	}, nil)

	_, err := te.Execute(context.Background(), "my-tool", `{"query":"test"}`)
	if err == nil {
		t.Fatal("expected error when no MCP host is configured")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected 'not initialized' in error, got: %v", err)
	}
}

func TestBuildPrompt(t *testing.T) {
	agentCfg := &config.AgentConfig{
		SystemPrompt: "You are helpful.",
		Memory:       config.MemoryConfig{SlidingWindow: 10},
	}

	history := []threads.Message{
		{Role: "user", Content: "Hi"},
		{Role: "assistant", Content: "Hello!"},
	}

	messages := BuildPrompt(agentCfg, history, "How are you?", nil)

	if len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(messages))
	}
	if messages[0].Role != "system" || messages[0].Content != "You are helpful." {
		t.Errorf("unexpected system message: %v", messages[0])
	}
	if messages[1].Role != "user" || messages[1].Content != "Hi" {
		t.Errorf("unexpected history user message: %v", messages[1])
	}
	if messages[2].Role != "assistant" || messages[2].Content != "Hello!" {
		t.Errorf("unexpected history assistant message: %v", messages[2])
	}
	if messages[3].Role != "user" || messages[3].Content != "How are you?" {
		t.Errorf("unexpected user message: %v", messages[3])
	}
}

func TestBuildPromptNoHistory(t *testing.T) {
	agentCfg := &config.AgentConfig{
		SystemPrompt: "You are helpful.",
	}

	messages := BuildPrompt(agentCfg, nil, "Hello", nil)

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Role != "system" {
		t.Errorf("expected system message first")
	}
	if messages[1].Role != "user" {
		t.Errorf("expected user message second")
	}
}

func TestBuildPromptEmptySystemPrompt(t *testing.T) {
	agentCfg := &config.AgentConfig{
		SystemPrompt: "",
	}

	messages := BuildPrompt(agentCfg, nil, "Hello", nil)

	// No system prompt = no system message.
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Role != "user" {
		t.Errorf("expected user message")
	}
}
