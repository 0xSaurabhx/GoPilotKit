package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultAnthropicBaseURL = "https://api.anthropic.com"
	anthropicVersion        = "2023-06-01"
	anthropicTimeout        = 120 * time.Second
)

// Anthropic implements the Provider interface for Anthropic's API.
type Anthropic struct {
	id      string
	model   string
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewAnthropic creates a new Anthropic provider.
func NewAnthropic(cfg ProviderConfig) (*Anthropic, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("anthropic: api_key is required")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultAnthropicBaseURL
	}
	return &Anthropic{
		id:      cfg.ID,
		model:   cfg.Model,
		apiKey:  cfg.APIKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: anthropicTimeout,
		},
	}, nil
}

func (a *Anthropic) ID() string   { return a.id }
func (a *Anthropic) Type() string { return "anthropic" }

func (a *Anthropic) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body, err := a.buildRequest(req, false)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: creating request: %w", err)
	}
	a.setHeaders(httpReq)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, a.parseError(resp.StatusCode, respBody)
	}

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return nil, fmt.Errorf("anthropic: parsing response: %w", err)
	}

	return a.convertResponse(&anthropicResp), nil
}

func (a *Anthropic) ChatCompletionStream(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, error) {
	body, err := a.buildRequest(req, true)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: creating request: %w", err)
	}
	a.setHeaders(httpReq)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: stream request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, a.parseError(resp.StatusCode, respBody)
	}

	chunks := make(chan ChatChunk, 64)
	go a.readStream(resp.Body, chunks)
	return chunks, nil
}

func (a *Anthropic) Close() error {
	a.client.CloseIdleConnections()
	return nil
}

func (a *Anthropic) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)
}

func (a *Anthropic) buildRequest(req *ChatRequest, stream bool) ([]byte, error) {
	if req.Model == "" {
		req.Model = a.model
	}

	// Separate system message from conversation messages
	var systemMsg string
	var messages []map[string]interface{}
	for _, m := range req.Messages {
		if m.Role == "system" {
			systemMsg = m.Content
			continue
		}
		msg := map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		}
		if len(m.ToolCalls) > 0 {
			msg["content"] = m.ToolCalls
		}
		if m.ToolCallID != "" {
			msg["content"] = []map[string]interface{}{
				{"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Content},
			}
		}
		messages = append(messages, msg)
	}

	payload := map[string]interface{}{
		"model":      req.Model,
		"messages":   messages,
		"max_tokens": 4096, // Anthropic requires max_tokens
		"stream":     stream,
	}
	if systemMsg != "" {
		payload["system"] = systemMsg
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		payload["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		payload["top_p"] = *req.TopP
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]interface{}, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]interface{}{
				"name":        t.Function.Name,
				"description": t.Function.Description,
				"input_schema": t.Function.Parameters,
			})
		}
		payload["tools"] = tools
	}

	return json.Marshal(payload)
}

func (a *Anthropic) convertResponse(resp *anthropicResponse) *ChatResponse {
	var text string
	var toolCalls []ToolCall

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			text += block.Text
		case "tool_use":
			toolCalls = append(toolCalls, ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: FunctionCall{
					Name:      block.Name,
					Arguments: string(block.Input),
				},
			})
		}
	}

	finishReason := "stop"
	if resp.StopReason == "tool_use" {
		finishReason = "tool_calls"
	}

	return &ChatResponse{
		ID: resp.ID,
		Choices: []Choice{
			{
				Index: 0,
				Message: Message{
					Role:      "assistant",
					Content:   text,
					ToolCalls: toolCalls,
				},
				FinishReason: finishReason,
			},
		},
		Usage: Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
		Model:      resp.Model,
		ProviderID: a.id,
	}
}

func (a *Anthropic) readStream(body io.ReadCloser, chunks chan<- ChatChunk) {
	defer close(chunks)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event anthropicStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content_block_delta":
			chunks <- ChatChunk{
				ID: event.MessageID,
				Choices: []ChunkChoice{
					{
						Index: 0,
						Delta: Message{
							Role:    "assistant",
							Content: event.Delta.Text,
						},
					},
				},
			}
		case "message_stop":
			return
		}
	}
}

func (a *Anthropic) parseError(statusCode int, body []byte) error {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	json.Unmarshal(body, &errResp)

	msg := errResp.Error.Message
	if msg == "" {
		msg = string(body)
	}

	retryable := statusCode == 429 || statusCode == 529 || statusCode == 500
	return &ProviderError{
		StatusCode: statusCode,
		Message:    msg,
		Provider:   "anthropic",
		Retryable:  retryable,
	}
}

// --- Anthropic-specific response types ---

type anthropicResponse struct {
	ID          string `json:"id"`
	Model       string `json:"model"`
	StopReason  string `json:"stop_reason"`
	Content     []struct {
		Type string `json:"type"`
		Text string `json:"text"`
		ID   string `json:"id,omitempty"`
		Name string `json:"name,omitempty"`
		Input json.RawMessage `json:"input,omitempty"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicStreamEvent struct {
	Type      string `json:"type"`
	MessageID string `json:"message_id,omitempty"`
	Delta     struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta,omitempty"`
}
