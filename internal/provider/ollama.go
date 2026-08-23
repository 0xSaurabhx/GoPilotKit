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
	defaultOllamaBaseURL = "http://localhost:11434"
	ollamaTimeout        = 300 * time.Second
)

// Ollama implements the Provider interface for Ollama's local API.
type Ollama struct {
	id      string
	model   string
	baseURL string
	client  *http.Client
}

// NewOllama creates a new Ollama provider.
func NewOllama(cfg ProviderConfig) (*Ollama, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultOllamaBaseURL
	}
	return &Ollama{
		id:      cfg.ID,
		model:   cfg.Model,
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: ollamaTimeout,
		},
	}, nil
}

func (o *Ollama) ID() string   { return o.id }
func (o *Ollama) Type() string { return "ollama" }

func (o *Ollama) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body, err := o.buildRequest(req, false)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama: creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ollama: reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &ProviderError{
			StatusCode: resp.StatusCode,
			Message:    string(respBody),
			Provider:   "ollama",
			Retryable:  resp.StatusCode >= 500,
		}
	}

	var ollamaResp ollamaResponse
	if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
		return nil, fmt.Errorf("ollama: parsing response: %w", err)
	}

	return o.convertResponse(&ollamaResp), nil
}

func (o *Ollama) ChatCompletionStream(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, error) {
	body, err := o.buildRequest(req, true)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama: creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: stream request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &ProviderError{
			StatusCode: resp.StatusCode,
			Message:    string(respBody),
			Provider:   "ollama",
			Retryable:  resp.StatusCode >= 500,
		}
	}

	chunks := make(chan ChatChunk, 64)
	go o.readStream(resp.Body, chunks)
	return chunks, nil
}

func (o *Ollama) Close() error {
	o.client.CloseIdleConnections()
	return nil
}

func (o *Ollama) buildRequest(req *ChatRequest, stream bool) ([]byte, error) {
	if req.Model == "" {
		req.Model = o.model
	}

	var messages []ollamaMessage
	for _, m := range req.Messages {
		messages = append(messages, ollamaMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	payload := map[string]interface{}{
		"model":    req.Model,
		"messages": messages,
		"stream":   stream,
	}
	if req.Temperature != nil {
		payload["options"] = map[string]interface{}{
			"temperature": *req.Temperature,
		}
	}

	return json.Marshal(payload)
}

func (o *Ollama) convertResponse(resp *ollamaResponse) *ChatResponse {
	return &ChatResponse{
		ID: fmt.Sprintf("ollama-%d", time.Now().UnixNano()),
		Choices: []Choice{
			{
				Index: 0,
				Message: Message{
					Role:    "assistant",
					Content: resp.Message.Content,
				},
				FinishReason: "stop",
			},
		},
		Usage: Usage{
			PromptTokens:     resp.PromptEvalCount,
			CompletionTokens: resp.EvalCount,
			TotalTokens:      resp.PromptEvalCount + resp.EvalCount,
		},
		Model:      resp.Model,
		ProviderID: o.id,
	}
}

func (o *Ollama) readStream(body io.ReadCloser, chunks chan<- ChatChunk) {
	defer close(chunks)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()

		var ollamaChunk ollamaResponse
		if err := json.Unmarshal([]byte(line), &ollamaChunk); err != nil {
			continue
		}

		chunks <- ChatChunk{
			ID: fmt.Sprintf("ollama-%d", time.Now().UnixNano()),
			Choices: []ChunkChoice{
				{
					Index: 0,
					Delta: Message{
						Role:    "assistant",
						Content: ollamaChunk.Message.Content,
					},
				},
			},
		}

		if ollamaChunk.Done {
			return
		}
	}
}

// --- Ollama-specific types ---

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaResponse struct {
	Model              string `json:"model"`
	Message            struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done               bool `json:"done"`
	PromptEvalCount    int  `json:"prompt_eval_count"`
	EvalCount          int  `json:"eval_count"`
}
