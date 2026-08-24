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
	defaultMistralBaseURL = "https://api.mistral.ai/v1"
	mistralTimeout        = 120 * time.Second
)

// Mistral implements the Provider interface for Mistral's API (OpenAI-compatible).
type Mistral struct {
	id      string
	model   string
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewMistral creates a new Mistral provider.
func NewMistral(cfg ProviderConfig) (*Mistral, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("mistral: api_key is required")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultMistralBaseURL
	}
	return &Mistral{
		id:      cfg.ID,
		model:   cfg.Model,
		apiKey:  cfg.APIKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: mistralTimeout,
		},
	}, nil
}

func (m *Mistral) ID() string   { return m.id }
func (m *Mistral) Type() string { return "mistral" }

func (m *Mistral) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	req.Stream = false
	body, err := m.buildRequest(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mistral: creating request: %w", err)
	}
	m.setHeaders(httpReq)

	resp, err := m.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mistral: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("mistral: reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, m.parseError(resp.StatusCode, respBody)
	}

	var result ChatResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("mistral: parsing response: %w", err)
	}
	result.ProviderID = m.id
	return &result, nil
}

func (m *Mistral) ChatCompletionStream(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, error) {
	req.Stream = true
	body, err := m.buildRequest(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mistral: creating request: %w", err)
	}
	m.setHeaders(httpReq)

	resp, err := m.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mistral: stream request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, m.parseError(resp.StatusCode, respBody)
	}

	chunks := make(chan ChatChunk, 64)
	go m.readStream(resp.Body, chunks)
	return chunks, nil
}

func (m *Mistral) Close() error {
	m.client.CloseIdleConnections()
	return nil
}

func (m *Mistral) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
}

func (m *Mistral) buildRequest(req *ChatRequest) ([]byte, error) {
	if req.Model == "" {
		req.Model = m.model
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

	return json.Marshal(payload)
}

func (m *Mistral) readStream(body io.ReadCloser, chunks chan<- ChatChunk) {
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

func (m *Mistral) parseError(statusCode int, body []byte) error {
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
		Provider:   "mistral",
		Retryable:  retryable,
	}
}
