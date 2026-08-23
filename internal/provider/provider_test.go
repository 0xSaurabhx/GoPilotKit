package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- Mock Provider for Testing ---

type mockProvider struct {
	id            string
响应           *ChatResponse
	streamChunks  []ChatChunk
	err           error
	callCount     int
}

func (m *mockProvider) ID() string   { return m.id }
func (m *mockProvider) Type() string { return "mock" }

func (m *mockProvider) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	return m.响应, nil
}

func (m *mockProvider) ChatCompletionStream(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, error) {
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan ChatChunk, len(m.streamChunks))
	for _, c := range m.streamChunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (m *mockProvider) Close() error { return nil }

// --- Provider Interface Tests ---

func TestProviderNewOpenAI(t *testing.T) {
	p, err := New(ProviderConfig{
		ID:     "test",
		Type:   "openai",
		Model:  "gpt-4o",
		APIKey: "sk-test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ID() != "test" {
		t.Errorf("expected ID 'test', got '%s'", p.ID())
	}
	if p.Type() != "openai" {
		t.Errorf("expected Type 'openai', got '%s'", p.Type())
	}
}

func TestProviderNewAnthropic(t *testing.T) {
	p, err := New(ProviderConfig{
		ID:     "test",
		Type:   "anthropic",
		Model:  "claude-3-5-sonnet",
		APIKey: "sk-ant-test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Type() != "anthropic" {
		t.Errorf("expected Type 'anthropic', got '%s'", p.Type())
	}
}

func TestProviderNewOllama(t *testing.T) {
	p, err := New(ProviderConfig{
		ID:    "test",
		Type:  "ollama",
		Model: "llama3.1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Type() != "ollama" {
		t.Errorf("expected Type 'ollama', got '%s'", p.Type())
	}
}

func TestProviderNewUnsupported(t *testing.T) {
	_, err := New(ProviderConfig{
		ID:   "test",
		Type: "unsupported",
	})
	if err == nil {
		t.Fatal("expected error for unsupported provider type")
	}
}

func TestProviderNewOpenAIMissingAPIKey(t *testing.T) {
	_, err := New(ProviderConfig{
		ID:   "test",
		Type: "openai",
	})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

func TestProviderNewAnthropicMissingAPIKey(t *testing.T) {
	_, err := New(ProviderConfig{
		ID:   "test",
		Type: "anthropic",
	})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

// --- OpenAI Live Server Test ---

func TestOpenAIChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("unexpected auth header")
		}

		resp := ChatResponse{
			ID: "test-id",
			Choices: []Choice{
				{
					Index: 0,
					Message: Message{
						Role:    "assistant",
						Content: "Hello from OpenAI!",
					},
					FinishReason: "stop",
				},
			},
			Usage: Usage{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
			Model: "gpt-4o",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p, err := NewOpenAI(ProviderConfig{
		ID:      "test-openai",
		Type:    "openai",
		Model:   "gpt-4o",
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer p.Close()

	resp, err := p.ChatCompletion(context.Background(), &ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Choices[0].Message.Content != "Hello from OpenAI!" {
		t.Errorf("unexpected content: %s", resp.Choices[0].Message.Content)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("expected 15 tokens, got %d", resp.Usage.TotalTokens)
	}
}

// --- Error Tests ---

func TestProviderError(t *testing.T) {
	err := &ProviderError{
		StatusCode: 429,
		Message:    "rate limited",
		Provider:   "openai",
		Retryable:  true,
	}
	if err.Error() != "openai provider error (HTTP 429): rate limited" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestRateLimitError(t *testing.T) {
	err := &RateLimitError{
		Provider:   "openai",
		RetryAfter: 30,
	}
	if err.Error() != "openai rate limited, retry after 30s" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

// --- Circuit Breaker Tests ---

func TestCircuitBreakerClosed(t *testing.T) {
	mock := &mockProvider{id: "mock", 响应: &ChatResponse{
		ID: "test",
		Choices: []Choice{{Message: Message{Content: "ok"}, FinishReason: "stop"}},
	}}

	cb := NewCircuitBreaker(mock, 3, time.Second)

	resp, err := cb.ChatCompletion(context.Background(), &ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Choices[0].Message.Content != "ok" {
		t.Errorf("unexpected content")
	}
	if cb.GetState() != CircuitClosed {
		t.Error("expected circuit to be closed")
	}
}

func TestCircuitBreakerOpens(t *testing.T) {
	mock := &mockProvider{
		id:  "mock",
		err: fmt.Errorf("provider error"),
	}

	cb := NewCircuitBreaker(mock, 3, time.Second)

	// Fail 3 times
	for i := 0; i < 3; i++ {
		cb.ChatCompletion(context.Background(), &ChatRequest{
			Messages: []Message{{Role: "user", Content: "test"}},
		})
	}

	if cb.GetState() != CircuitOpen {
		t.Error("expected circuit to be open")
	}

	// Should reject next request
	_, err := cb.ChatCompletion(context.Background(), &ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
	})
	if err == nil {
		t.Error("expected error when circuit is open")
	}
}

func TestCircuitBreakerHalfOpen(t *testing.T) {
	mock := &mockProvider{id: "mock", err: fmt.Errorf("error")}

	cb := NewCircuitBreaker(mock, 1, 50*time.Millisecond)

	// Open the circuit
	cb.ChatCompletion(context.Background(), &ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
	})

	if cb.GetState() != CircuitOpen {
		t.Error("expected circuit to be open")
	}

	// Wait for reset timeout
	time.Sleep(100 * time.Millisecond)

	// Now provider is fixed
	mock.err = nil
	mock.响应 = &ChatResponse{
		ID: "test",
		Choices: []Choice{{Message: Message{Content: "recovered"}, FinishReason: "stop"}},
	}

	resp, err := cb.ChatCompletion(context.Background(), &ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Choices[0].Message.Content != "recovered" {
		t.Error("expected recovered response")
	}
}

// --- Fallback Provider Tests ---

func TestFallbackProviderPrimarySuccess(t *testing.T) {
	primary := &mockProvider{
		id: "primary",
		响应: &ChatResponse{
			ID: "primary-resp",
			Choices: []Choice{{Message: Message{Content: "from primary"}}},
		},
	}
	fallback := &mockProvider{id: "fallback"}

	fp := NewFallbackProvider(primary, fallback)

	resp, err := fp.ChatCompletion(context.Background(), &ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Choices[0].Message.Content != "from primary" {
		t.Errorf("expected 'from primary', got '%s'", resp.Choices[0].Message.Content)
	}
	if primary.callCount != 1 {
		t.Errorf("expected primary to be called once, got %d", primary.callCount)
	}
	if fallback.callCount != 0 {
		t.Errorf("expected fallback to not be called, got %d", fallback.callCount)
	}
}

func TestFallbackProviderFallsBack(t *testing.T) {
	primary := &mockProvider{
		id:  "primary",
		err: &ProviderError{StatusCode: 503, Message: "unavailable", Provider: "primary", Retryable: true},
	}
	fallback := &mockProvider{
		id: "fallback",
		响应: &ChatResponse{
			ID: "fallback-resp",
			Choices: []Choice{{Message: Message{Content: "from fallback"}}},
		},
	}

	fp := NewFallbackProvider(primary, fallback)

	resp, err := fp.ChatCompletion(context.Background(), &ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Choices[0].Message.Content != "from fallback" {
		t.Errorf("expected 'from fallback', got '%s'", resp.Choices[0].Message.Content)
	}
}

func TestFallbackProviderAllFail(t *testing.T) {
	primary := &mockProvider{
		id:  "primary",
		err: &ProviderError{StatusCode: 503, Message: "error", Provider: "primary", Retryable: true},
	}
	fallback := &mockProvider{
		id:  "fallback",
		err: &ProviderError{StatusCode: 503, Message: "error", Provider: "fallback", Retryable: true},
	}

	fp := NewFallbackProvider(primary, fallback)

	_, err := fp.ChatCompletion(context.Background(), &ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
	})
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
}

func TestFallbackProviderNonRetryableFailsImmediately(t *testing.T) {
	primary := &mockProvider{
		id:  "primary",
		err: &ProviderError{StatusCode: 401, Message: "unauthorized", Provider: "primary", Retryable: false},
	}
	fallback := &mockProvider{
		id: "fallback",
		响应: &ChatResponse{
			Choices: []Choice{{Message: Message{Content: "from fallback"}}},
		},
	}

	fp := NewFallbackProvider(primary, fallback)

	_, err := fp.ChatCompletion(context.Background(), &ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
	})
	// Non-retryable error from primary should NOT fall back
	if err == nil {
		t.Error("expected error for non-retryable failure")
	}
}

// --- Chat Request/Response Serialization Tests ---

func TestChatRequestMarshal(t *testing.T) {
	temp := 0.7
	req := &ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		},
		Model:       "gpt-4o",
		MaxTokens:   1000,
		Temperature: &temp,
		Stream:      false,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed ChatRequest
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.Model != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %s", parsed.Model)
	}
	if parsed.MaxTokens != 1000 {
		t.Errorf("expected max_tokens 1000, got %d", parsed.MaxTokens)
	}
}

func TestMessageWithToolCalls(t *testing.T) {
	msg := Message{
		Role:    "assistant",
		Content: "",
		ToolCalls: []ToolCall{
			{
				ID:   "call_123",
				Type: "function",
				Function: FunctionCall{
					Name:      "search",
					Arguments: `{"query":"hello"}`,
				},
			},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed Message
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(parsed.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(parsed.ToolCalls))
	}
	if parsed.ToolCalls[0].Function.Name != "search" {
		t.Errorf("expected function name 'search', got '%s'", parsed.ToolCalls[0].Function.Name)
	}
}
