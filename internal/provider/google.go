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
	defaultGoogleBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	googleTimeout        = 120 * time.Second
)

// Google implements the Provider interface for Google Gemini API.
type Google struct {
	id      string
	model   string
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewGoogle creates a new Google Gemini provider.
func NewGoogle(cfg ProviderConfig) (*Google, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("google: api_key is required")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultGoogleBaseURL
	}
	return &Google{
		id:      cfg.ID,
		model:   cfg.Model,
		apiKey:  cfg.APIKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: googleTimeout,
		},
	}, nil
}

func (g *Google) ID() string   { return g.id }
func (g *Google) Type() string { return "google" }

func (g *Google) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body, err := g.buildRequest(req, false)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", g.baseURL, g.model, g.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("google: creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("google: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("google: reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, g.parseError(resp.StatusCode, respBody)
	}

	return g.parseGenerateResponse(respBody, req)
}

func (g *Google) ChatCompletionStream(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, error) {
	body, err := g.buildRequest(req, true)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse&key=%s", g.baseURL, g.model, g.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("google: creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("google: stream request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, g.parseError(resp.StatusCode, respBody)
	}

	chunks := make(chan ChatChunk, 64)
	go g.readStream(resp.Body, chunks)
	return chunks, nil
}

func (g *Google) Close() error {
	g.client.CloseIdleConnections()
	return nil
}

// buildRequest converts our internal format to Google's GenerateContent format.
func (g *Google) buildRequest(req *ChatRequest, stream bool) ([]byte, error) {
	// Convert messages to Google format.
	var contents []googleContent
	var systemInstruction *googleContent

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemInstruction = &googleContent{
				Parts: []googlePart{{Text: msg.Content}},
			}
			continue
		}

		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}

		content := googleContent{
			Role:  role,
			Parts: []googlePart{{Text: msg.Content}},
		}

		// Handle tool calls.
		if len(msg.ToolCalls) > 0 {
			functionCalls := make([]googleFunctionCall, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				var args map[string]interface{}
				json.Unmarshal([]byte(tc.Function.Arguments), &args)
				functionCalls = append(functionCalls, googleFunctionCall{
					Name: tc.Function.Name,
					Args: args,
				})
			}
			content.Parts = append(content.Parts, googlePart{FunctionCall: &functionCalls[0]})
		}

		// Handle tool results.
		if msg.ToolCallID != "" {
			content.Parts = []googlePart{{FunctionResponse: &googleFunctionResponse{
				Name:     msg.Name,
				Response: map[string]string{"result": msg.Content},
			}}}
		}

		contents = append(contents, content)
	}

	payload := map[string]interface{}{
		"contents": contents,
	}
	if systemInstruction != nil {
		payload["systemInstruction"] = systemInstruction
	}

	// Add generation config.
	genConfig := map[string]interface{}{}
	if req.MaxTokens > 0 {
		genConfig["maxOutputTokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		genConfig["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		genConfig["topP"] = *req.TopP
	}
	if len(genConfig) > 0 {
		payload["generationConfig"] = genConfig
	}

	// Add tools.
	if len(req.Tools) > 0 {
		var googleTools []googleTool
		for _, tool := range req.Tools {
			googleTools = append(googleTools, googleTool{
				FunctionDeclarations: []googleFunctionDecl{{
					Name:        tool.Function.Name,
					Description: tool.Function.Description,
					Parameters:  tool.Function.Parameters,
				}},
			})
		}
		payload["tools"] = googleTools
	}

	return json.Marshal(payload)
}

// parseGenerateResponse parses a non-streaming Google response.
func (g *Google) parseGenerateResponse(body []byte, req *ChatRequest) (*ChatResponse, error) {
	var googleResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string          `json:"text"`
					FunctionCall *googleFunctionCall `json:"functionCall"`
				} `json:"parts"`
				Role string `json:"role"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}

	if err := json.Unmarshal(body, &googleResp); err != nil {
		return nil, fmt.Errorf("google: parsing response: %w", err)
	}

	if len(googleResp.Candidates) == 0 {
		return nil, fmt.Errorf("google: no candidates in response")
	}

	candidate := googleResp.Candidates[0]
	var text string
	var toolCalls []ToolCall

	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			text += part.Text
		}
		if part.FunctionCall != nil {
			argsJSON, _ := json.Marshal(part.FunctionCall.Args)
			toolCalls = append(toolCalls, ToolCall{
				ID:   fmt.Sprintf("call_%d", len(toolCalls)),
				Type: "function",
				Function: FunctionCall{
					Name:      part.FunctionCall.Name,
					Arguments: string(argsJSON),
				},
			})
		}
	}

	msg := Message{
		Role:       "assistant",
		Content:    text,
		ToolCalls:  toolCalls,
	}

	finishReason := candidate.FinishReason
	return &ChatResponse{
		ID: fmt.Sprintf("google-%d", time.Now().UnixNano()),
		Choices: []Choice{{
			Index:        0,
			Message:      msg,
			FinishReason: finishReason,
		}},
		Usage: Usage{
			PromptTokens:     googleResp.UsageMetadata.PromptTokenCount,
			CompletionTokens: googleResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      googleResp.UsageMetadata.TotalTokenCount,
		},
		Model:      g.model,
		ProviderID: g.id,
	}, nil
}

// readStream reads streaming SSE responses from Google.
func (g *Google) readStream(body io.ReadCloser, chunks chan<- ChatChunk) {
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

		var googleResp struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text         string          `json:"text"`
						FunctionCall *googleFunctionCall `json:"functionCall"`
					} `json:"parts"`
				} `json:"content"`
				FinishReason string `json:"finishReason"`
			} `json:"candidates"`
			UsageMetadata struct {
				PromptTokenCount     int `json:"promptTokenCount"`
				CandidatesTokenCount int `json:"candidatesTokenCount"`
				TotalTokenCount      int `json:"totalTokenCount"`
			} `json:"usageMetadata"`
		}

		if err := json.Unmarshal([]byte(data), &googleResp); err != nil {
			continue
		}

		if len(googleResp.Candidates) == 0 {
			continue
		}

		candidate := googleResp.Candidates[0]
		var delta Message

		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				delta.Content += part.Text
			}
			if part.FunctionCall != nil {
				argsJSON, _ := json.Marshal(part.FunctionCall.Args)
				delta.ToolCalls = append(delta.ToolCalls, ToolCall{
					ID:   "call_0",
					Type: "function",
					Function: FunctionCall{
						Name:      part.FunctionCall.Name,
						Arguments: string(argsJSON),
					},
				})
			}
		}

		finishReason := candidate.FinishReason

		chunk := ChatChunk{
			ID: fmt.Sprintf("google-%d", time.Now().UnixNano()),
			Choices: []ChunkChoice{{
				Index:        0,
				Delta:        delta,
				FinishReason: &finishReason,
			}},
		}

		if googleResp.UsageMetadata.TotalTokenCount > 0 {
			chunk.Usage = &Usage{
				PromptTokens:     googleResp.UsageMetadata.PromptTokenCount,
				CompletionTokens: googleResp.UsageMetadata.CandidatesTokenCount,
				TotalTokens:      googleResp.UsageMetadata.TotalTokenCount,
			}
		}

		chunks <- chunk
	}
}

func (g *Google) parseError(statusCode int, body []byte) error {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
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
		Provider:   "google",
		Retryable:  retryable,
	}
}

// Google-specific types.

type googleContent struct {
	Role  string        `json:"role,omitempty"`
	Parts []googlePart  `json:"parts"`
}

type googlePart struct {
	Text             string                `json:"text,omitempty"`
	FunctionCall     *googleFunctionCall   `json:"functionCall,omitempty"`
	FunctionResponse *googleFunctionResponse `json:"functionResponse,omitempty"`
}

type googleFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type googleFunctionResponse struct {
	Name     string      `json:"name"`
	Response interface{} `json:"response"`
}

type googleTool struct {
	FunctionDeclarations []googleFunctionDecl `json:"functionDeclarations"`
}

type googleFunctionDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}
