package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// Provider defines the interface for LLM connectors.
// All providers (OpenAI, Anthropic, Ollama, etc.) implement this interface.
type Provider interface {
	// ID returns the unique identifier for this provider instance.
	ID() string

	// Type returns the provider type (openai, anthropic, ollama, etc.).
	Type() string

	// ChatCompletion sends a chat completion request and returns the full response.
	ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error)

	// ChatCompletionStream sends a chat completion request and returns a stream.
	ChatCompletionStream(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, error)

	// Close performs any cleanup (closing HTTP clients, etc.)
	Close() error
}

// ChatRequest represents a chat completion request.
type ChatRequest struct {
	Messages    []Message      `json:"messages"`
	Model       string         `json:"model"`
	MaxTokens   int            `json:"max_tokens,omitempty"`
	Temperature *float64       `json:"temperature,omitempty"`
	TopP        *float64       `json:"top_p,omitempty"`
	Stream      bool           `json:"stream,omitempty"`
	Tools       []ToolDef      `json:"tools,omitempty"`
	ToolChoice  interface{}    `json:"tool_choice,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
	Meta        map[string]interface{} `json:"-"`
}

// ResponseFormat specifies the format for structured output.
type ResponseFormat struct {
	Type       string          `json:"type"`
	JSONSchema *JSONSchema     `json:"json_schema,omitempty"`
}

// JSONSchema defines a JSON schema for structured output.
type JSONSchema struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict,omitempty"`
}

// Message represents a chat message.
type Message struct {
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
}

// ToolDef defines a tool available to the model.
type ToolDef struct {
	Type     string       `json:"type"`
	Function FunctionDef  `json:"function"`
}

// FunctionDef defines a function tool.
type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ToolCall represents a tool call in the model's response.
type ToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function FunctionCall    `json:"function"`
}

// FunctionCall represents the function call details.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatResponse represents the complete chat completion response.
type ChatResponse struct {
	ID                string        `json:"id"`
	Choices           []Choice      `json:"choices"`
	Usage             Usage         `json:"usage"`
	Model             string        `json:"model"`
	ProviderID        string        `json:"provider_id,omitempty"`
}

// Choice represents a single completion choice.
type Choice struct {
	Index        int       `json:"index"`
	Message      Message   `json:"message"`
	FinishReason string    `json:"finish_reason"`
}

// Usage tracks token consumption.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatChunk represents a single chunk in a streaming response.
type ChatChunk struct {
	ID     string   `json:"id"`
	Choices []ChunkChoice `json:"choices"`
	Usage  *Usage  `json:"usage,omitempty"`
}

// ChunkChoice represents a single choice in a streaming chunk.
type ChunkChoice struct {
	Index        int       `json:"index"`
	Delta        Message   `json:"delta"`
	FinishReason *string   `json:"finish_reason"`
}

// ProviderConfig holds the configuration for creating a provider.
type ProviderConfig struct {
	ID      string
	Type    string
	Model   string
	APIKey  string
	BaseURL string
}

// New creates a new provider instance based on the configuration.
func New(cfg ProviderConfig) (Provider, error) {
	switch cfg.Type {
	case "openai":
		return NewOpenAI(cfg)
	case "anthropic":
		return NewAnthropic(cfg)
	case "ollama":
		return NewOllama(cfg)
	case "google":
		return NewGoogle(cfg)
	case "mistral":
		return NewMistral(cfg)
	case "cohere":
		return NewCohere(cfg)
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", cfg.Type)
	}
}

// --- Error Types ---

// ProviderError represents an error from an LLM provider.
type ProviderError struct {
	StatusCode int
	Message    string
	Provider   string
	Retryable  bool
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("%s provider error (HTTP %d): %s", e.Provider, e.StatusCode, e.Message)
}

// RateLimitError indicates a rate limit was hit.
type RateLimitError struct {
	Provider   string
	RetryAfter int // seconds
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("%s rate limited, retry after %ds", e.Provider, e.RetryAfter)
}

// --- Helpers ---

// ReadAll is a convenience for io.ReadAll.
var ReadAll = io.ReadAll
