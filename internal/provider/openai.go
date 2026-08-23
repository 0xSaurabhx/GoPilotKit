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
	defaultOpenAIBaseURL = "https://api.openai.com/v1"
	openAITimeout        = 120 * time.Second
)

// OpenAI implements the Provider interface for OpenAI's API.
type OpenAI struct {
	id      string
	model   string
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewOpenAI creates a new OpenAI provider.
func NewOpenAI(cfg ProviderConfig) (*OpenAI, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("openai: api_key is required")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	return &OpenAI{
		id:      cfg.ID,
		model:   cfg.Model,
		apiKey:  cfg.APIKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: openAITimeout,
		},
	}, nil
}

func (o *OpenAI) ID() string   { return o.id }
func (o *OpenAI) Type() string { return "openai" }

func (o *OpenAI) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	req.Stream = false
	body, err := o.buildRequest(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: creating request: %w", err)
	}
	o.setHeaders(httpReq)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai: reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, o.parseError(resp.StatusCode, respBody)
	}

	var result ChatResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("openai: parsing response: %w", err)
	}
	result.ProviderID = o.id
	return &result, nil
}

func (o *OpenAI) ChatCompletionStream(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, error) {
	req.Stream = true
	body, err := o.buildRequest(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: creating request: %w", err)
	}
	o.setHeaders(httpReq)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: stream request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, o.parseError(resp.StatusCode, respBody)
	}

	chunks := make(chan ChatChunk, 64)
	go o.readStream(resp.Body, chunks)
	return chunks, nil
}

func (o *OpenAI) Close() error {
	o.client.CloseIdleConnections()
	return nil
}

func (o *OpenAI) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
}

func (o *OpenAI) buildRequest(req *ChatRequest) ([]byte, error) {
	// Set model from provider config if not specified
	if req.Model == "" {
		req.Model = o.model
	}

	payload := map[string]interface{}{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   req.Stream,
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
		payload["tools"] = req.Tools
	}
	if req.ToolChoice != nil {
		payload["tool_choice"] = req.ToolChoice
	}
	if req.ResponseFormat != nil {
		payload["response_format"] = req.ResponseFormat
	}

	return json.Marshal(payload)
}

func (o *OpenAI) readStream(body io.ReadCloser, chunks chan<- ChatChunk) {
	defer close(chunks)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return
		}

		var chunk ChatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		chunks <- chunk
	}
}

func (o *OpenAI) parseError(statusCode int, body []byte) error {
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

	retryable := statusCode == 429 || statusCode == 503 || statusCode == 500
	return &ProviderError{
		StatusCode: statusCode,
		Message:    msg,
		Provider:   "openai",
		Retryable:  retryable,
	}
}
