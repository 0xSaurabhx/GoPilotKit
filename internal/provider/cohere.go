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
	defaultCohereBaseURL = "https://api.cohere.com/v2"
	cohereTimeout        = 120 * time.Second
)

// Cohere implements the Provider interface for Cohere's Chat API.
type Cohere struct {
	id      string
	model   string
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewCohere creates a new Cohere provider.
func NewCohere(cfg ProviderConfig) (*Cohere, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("cohere: api_key is required")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultCohereBaseURL
	}
	return &Cohere{
		id:      cfg.ID,
		model:   cfg.Model,
		apiKey:  cfg.APIKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: cohereTimeout,
		},
	}, nil
}

func (c *Cohere) ID() string   { return c.id }
func (c *Cohere) Type() string { return "cohere" }

func (co *Cohere) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body, err := co.buildRequest(req, false)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, co.baseURL+"/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("cohere: creating request: %w", err)
	}
	co.setHeaders(httpReq)

	resp, err := co.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("cohere: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cohere: reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, co.parseError(resp.StatusCode, respBody)
	}

	return co.parseResponse(respBody, req)
}

func (co *Cohere) ChatCompletionStream(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, error) {
	body, err := co.buildRequest(req, true)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, co.baseURL+"/chat/stream", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("cohere: creating request: %w", err)
	}
	co.setHeaders(httpReq)

	resp, err := co.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("cohere: stream request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, co.parseError(resp.StatusCode, respBody)
	}

	chunks := make(chan ChatChunk, 64)
	go co.readStream(resp.Body, chunks)
	return chunks, nil
}

func (co *Cohere) Close() error {
	co.client.CloseIdleConnections()
	return nil
}

func (co *Cohere) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+co.apiKey)
}

// buildRequest converts our internal format to Cohere's v2 chat format.
func (co *Cohere) buildRequest(req *ChatRequest, stream bool) ([]byte, error) {
	// Extract system message.
	var systemMsgs []string
	var chatMessages []cohereMessage

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemMsgs = append(systemMsgs, msg.Content)
			continue
		}

		role := "user"
		if msg.Role == "assistant" {
			role = "assistant"
		}

		cm := cohereMessage{
			Role: role,
		}

		if msg.Content != "" {
			cm.Content = &cohereMessageContent{
				Text: msg.Content,
			}
		}

		// Handle tool calls.
		if len(msg.ToolCalls) > 0 {
			cm.ToolCalls = make([]cohereToolCall, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				cm.ToolCalls = append(cm.ToolCalls, cohereToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: cohereFunctionCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
		}

		// Handle tool results.
		if msg.ToolCallID != "" {
			cm.ToolResults = []cohereToolResult{{
				CallID: msg.ToolCallID,
				Output: msg.Content,
			}}
		}

		chatMessages = append(chatMessages, cm)
	}

	payload := map[string]interface{}{
		"model":    co.model,
		"messages": chatMessages,
		"stream":   stream,
	}

	if len(systemMsgs) > 0 {
		payload["preamble"] = strings.Join(systemMsgs, "\n")
	}

	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		payload["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		payload["p"] = *req.TopP
	}

	// Add tools.
	if len(req.Tools) > 0 {
		var cohereTools []cohereTool
		for _, tool := range req.Tools {
			cohereTools = append(cohereTools, cohereTool{
				Type: "function",
				Function: cohereToolFunction{
					Name:        tool.Function.Name,
					Description: tool.Function.Description,
					Parameters:  tool.Function.Parameters,
				},
			})
		}
		payload["tools"] = cohereTools
	}

	return json.Marshal(payload)
}

// parseResponse parses a non-streaming Cohere response.
func (co *Cohere) parseResponse(body []byte, req *ChatRequest) (*ChatResponse, error) {
	var cohereResp struct {
		ID        string `json:"id"`
		Message   struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"toolCalls"`
		} `json:"message"`
		FinishReason string `json:"finishReason"`
		Usage        struct {
			InputTokens  int `json:"inputTokens"`
			OutputTokens int `json:"outputTokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(body, &cohereResp); err != nil {
		return nil, fmt.Errorf("cohere: parsing response: %w", err)
	}

	var text string
	for _, c := range cohereResp.Message.Content {
		text += c.Text
	}

	var toolCalls []ToolCall
	for _, tc := range cohereResp.Message.ToolCalls {
		toolCalls = append(toolCalls, ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: FunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}

	msg := Message{
		Role:      "assistant",
		Content:   text,
		ToolCalls: toolCalls,
	}

	return &ChatResponse{
		ID: cohereResp.ID,
		Choices: []Choice{{
			Index:        0,
			Message:      msg,
			FinishReason: cohereResp.FinishReason,
		}},
		Usage: Usage{
			PromptTokens:     cohereResp.Usage.InputTokens,
			CompletionTokens: cohereResp.Usage.OutputTokens,
			TotalTokens:      cohereResp.Usage.InputTokens + cohereResp.Usage.OutputTokens,
		},
		Model:      co.model,
		ProviderID: co.id,
	}, nil
}

// readStream reads streaming SSE responses from Cohere.
func (co *Cohere) readStream(body io.ReadCloser, chunks chan<- ChatChunk) {
	defer close(chunks)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	var messageID string

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return
		}

		var event struct {
			Type    string `json:"type"`
			Delta   *struct {
				Message *struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
					ToolCalls []struct {
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"toolCalls"`
				} `json:"message"`
			} `json:"delta"`
			Message *struct {
				ToolCalls []struct {
					ID   string `json:"id"`
					Type string `json:"type"`
				} `json:"toolCalls"`
			} `json:"message"`
			FinishReason *string `json:"finishReason"`
			Usage        *struct {
				InputTokens  int `json:"inputTokens"`
				OutputTokens int `json:"outputTokens"`
			} `json:"usage"`
		}

		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		if event.Type == "message-start" && event.Message != nil {
			// We don't have the ID in message-start, set a default.
			messageID = fmt.Sprintf("cohere-%d", time.Now().UnixNano())
			continue
		}

		var delta Message
		var finishReason string

		if event.Delta != nil && event.Delta.Message != nil {
			for _, c := range event.Delta.Message.Content {
				delta.Content += c.Text
			}
			for _, tc := range event.Delta.Message.ToolCalls {
				delta.ToolCalls = append(delta.ToolCalls, ToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: FunctionCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
		}

		if event.FinishReason != nil {
			finishReason = *event.FinishReason
		}

		if delta.Content != "" || len(delta.ToolCalls) > 0 || finishReason != "" {
			chunk := ChatChunk{
				ID: messageID,
				Choices: []ChunkChoice{{
					Index:        0,
					Delta:        delta,
					FinishReason: &finishReason,
				}},
			}

			if event.Usage != nil {
				chunk.Usage = &Usage{
					PromptTokens:     event.Usage.InputTokens,
					CompletionTokens: event.Usage.OutputTokens,
					TotalTokens:      event.Usage.InputTokens + event.Usage.OutputTokens,
				}
			}

			chunks <- chunk
		}
	}
}

func (co *Cohere) parseError(statusCode int, body []byte) error {
	var errResp struct {
		Message string `json:"message"`
	}
	json.Unmarshal(body, &errResp)

	msg := errResp.Message
	if msg == "" {
		msg = string(body)
	}

	retryable := statusCode == 429 || statusCode == 503 || statusCode == 500
	return &ProviderError{
		StatusCode: statusCode,
		Message:    msg,
		Provider:   "cohere",
		Retryable:  retryable,
	}
}

// Cohere-specific types.

type cohereMessage struct {
	Role       string                `json:"role"`
	Content    *cohereMessageContent `json:"content,omitempty"`
	ToolCalls  []cohereToolCall      `json:"toolCalls,omitempty"`
	ToolResults []cohereToolResult   `json:"toolResults,omitempty"`
}

type cohereMessageContent struct {
	Text string `json:"text"`
}

type cohereToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function cohereFunctionCall `json:"function"`
}

type cohereFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type cohereToolResult struct {
	CallID string `json:"callId"`
	Output string `json:"output"`
}

type cohereTool struct {
	Type     string             `json:"type"`
	Function cohereToolFunction `json:"function"`
}

type cohereToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}
