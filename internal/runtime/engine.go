// Package runtime implements the graph-based agent execution engine.
//
// The Engine is the central orchestrator that connects the AG-UI protocol,
// LLM providers, thread store, and tool executor into a working system.
// It receives a user message, builds the prompt, calls the LLM, streams
// tokens as AG-UI events, handles tool calls, and persists everything.
package runtime

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/0xsaurabhx/GoPilotKit/internal/config"
	"github.com/0xsaurabhx/GoPilotKit/internal/protocol/agui"
	"github.com/0xsaurabhx/GoPilotKit/internal/provider"
	"github.com/0xsaurabhx/GoPilotKit/internal/store/threads"
)

const (
	// maxToolRounds is the maximum number of tool-call rounds per run.
	// Prevents infinite loops when the model keeps requesting tools.
	maxToolRounds = 10
)

// Engine is the runtime orchestrator. It wires together the LLM providers,
// thread store, and tool executor to handle agent runs end-to-end.
type Engine struct {
	providers map[string]provider.Provider   // provider ID → provider
	fallbacks map[string]provider.Provider   // agent name → fallback provider (wrapped)
	store     threads.Store
	agents    map[string]*config.AgentConfig // agent name → config
	tools     *ToolExecutor

	// Default sliding window for conversation history.
	defaultWindowSize int
}

// NewEngine creates a new runtime engine from the loaded configuration.
// It initializes all providers, opens the thread store, and sets up the tool executor.
func NewEngine(cfg *config.IntelligenceConfig) (*Engine, error) {
	e := &Engine{
		providers:         make(map[string]provider.Provider),
		fallbacks:         make(map[string]provider.Provider),
		agents:            make(map[string]*config.AgentConfig),
		defaultWindowSize: 20,
	}

	// Initialize LLM providers with circuit breakers.
	for _, pc := range cfg.Providers {
		p, err := provider.New(provider.ProviderConfig{
			ID:      pc.ID,
			Type:    pc.Type,
			Model:   pc.Model,
			APIKey:  pc.APIKey,
			BaseURL: pc.BaseURL,
		})
		if err != nil {
			// Close any providers we already opened.
			e.Close()
			return nil, fmt.Errorf("initializing provider %q: %w", pc.ID, err)
		}
		// Wrap in circuit breaker: open after 5 failures, reset after 30s.
		e.providers[pc.ID] = provider.NewCircuitBreaker(p, 5, 30*time.Second)
		log.Printf("runtime: provider %s (%s/%s) ready", pc.ID, pc.Type, pc.Model)
	}

	// Build fallback providers for agents that have a fallback configured.
	for _, ac := range cfg.Agents {
		e.agents[ac.Name] = &ac
		if ac.FallbackProvider != "" {
			primary, ok := e.providers[ac.Provider]
			if !ok {
				e.Close()
				return nil, fmt.Errorf("agent %q: provider %q not found", ac.Name, ac.Provider)
			}
			fallback, ok := e.providers[ac.FallbackProvider]
			if !ok {
				e.Close()
				return nil, fmt.Errorf("agent %q: fallback provider %q not found", ac.Name, ac.FallbackProvider)
			}
			e.fallbacks[ac.Name] = provider.NewFallbackProvider(primary, fallback)
		}
	}

	// Open the thread store.
	switch cfg.Storage.Driver {
	case "sqlite":
		store, err := threads.NewSQLiteStore(cfg.Storage.ConnectionURI)
		if err != nil {
			e.Close()
			return nil, fmt.Errorf("opening thread store: %w", err)
		}
		e.store = store
	default:
		e.Close()
		return nil, fmt.Errorf("unsupported storage driver: %s", cfg.Storage.Driver)
	}

	// Initialize tool executor (currently a stub; real tools need MCP/WASM).
	e.tools = NewToolExecutor(cfg.Tools)

	log.Printf("runtime: engine ready (providers=%d, agents=%d, tools=%d)",
		len(e.providers), len(e.agents), len(cfg.Tools))

	return e, nil
}

// HandleRun is the AG-UI RunFunc that the coordinator calls for every request.
// It orchestrates the full lifecycle: history load → prompt build → LLM stream
// → tool loop → persistence.
func (e *Engine) HandleRun(req agui.RunRequest, stream *agui.RunStream) {
	runID := stream.RunID()
	threadID := stream.ThreadID()

	// Look up agent config.
	agentCfg, ok := e.agents[req.Agent]
	if !ok {
		_ = stream.SendRunError(
			fmt.Sprintf("agent %q not configured", req.Agent),
			"AGENT_NOT_FOUND", false,
		)
		return
	}

	// Resolve the provider for this agent.
	prov := e.resolveProvider(req.Agent)

	// Ensure thread exists, create if new.
	thread, err := e.ensureThread(threadID, req.Agent)
	if err != nil {
		_ = stream.SendRunError(
			fmt.Errorf("thread error: %w", err).Error(),
			"THREAD_ERROR", true,
		)
		return
	}

	// Persist the user message.
	if err := e.persistUserMessage(threadID, req.Message, req.State); err != nil {
		log.Printf("runtime: failed to persist user message: %v", err)
		// Non-fatal — continue with the run.
	}

	// Persist run record.
	now := time.Now().UTC()
	runRecord := &threads.Run{
		ID:        runID,
		ThreadID:  threadID,
		Agent:     req.Agent,
		Status:    "running",
		StartedAt: now,
	}
	_ = e.store.CreateRun(runRecord)

	// Load conversation history.
	windowSize := agentCfg.Memory.SlidingWindow
	if windowSize <= 0 {
		windowSize = e.defaultWindowSize
	}
	history, err := e.store.GetMessages(threadID, windowSize)
	if err != nil {
		log.Printf("runtime: failed to load history: %v", err)
		history = nil // Continue without history.
	}

	// Build prompt messages.
	messages := BuildPrompt(agentCfg, history, req.Message, req.State)

	// Execute the LLM call with streaming.
	usage, err := e.streamLLM(req, stream, prov, messages, agentCfg, thread)

	// Update run record.
	endTime := time.Now().UTC()
	runRecord.EndedAt = &endTime
	if err != nil {
		runRecord.Status = "error"
		runRecord.Error = err.Error()
		_ = e.store.UpdateRun(runRecord)
		return
	}
	runRecord.Status = "completed"
	_ = e.store.UpdateRun(runRecord)

	// Send RUN_END with token usage.
	_ = stream.SendRunEnd(usage)

	// Update thread timestamp.
	thread.UpdatedAt = time.Now().UTC()
	_ = e.store.UpdateThread(thread)
}

// streamLLM calls the LLM provider with streaming and relays chunks as AG-UI events.
// It also handles the tool-call loop: if the model requests tools, it executes them
// and feeds the results back for another round.
func (e *Engine) streamLLM(
	req agui.RunRequest,
	stream *agui.RunStream,
	prov provider.Provider,
	messages []provider.Message,
	agentCfg *config.AgentConfig,
	thread *threads.Thread,
) (*agui.TokenUsage, error) {

	totalUsage := &agui.TokenUsage{}
	var allMessages []provider.Message // Accumulated messages across tool rounds.

	for round := 0; round <= maxToolRounds; round++ {
		// Check for cancellation.
		select {
		case <-stream.Cancelled():
			return totalUsage, fmt.Errorf("run cancelled")
		default:
		}

		// Use the stream's context (cancelled when client disconnects).
		runCtx := stream.Context()

		// Call the LLM with streaming.
		chunks, err := prov.ChatCompletionStream(runCtx, &provider.ChatRequest{
			Messages: messages,
			Stream:   true,
		})
		if err != nil {
			return totalUsage, fmt.Errorf("LLM stream error: %w", err)
		}

		// Accumulate the full assistant response.
		var fullContent string
		var toolCalls []provider.ToolCall

		// Send TEXT_START.
		msgID := fmt.Sprintf("msg_%d", time.Now().UnixNano())
		_ = stream.SendTextStart(msgID)

		// Consume the stream.
		for chunk := range chunks {
			if len(chunk.Choices) == 0 {
				continue
			}
			delta := chunk.Choices[0].Delta

			// Accumulate text content.
			if delta.Content != "" {
				fullContent += delta.Content
				_ = stream.SendTextChunk(delta.Content)
			}

			// Accumulate tool calls.
			for _, tc := range delta.ToolCalls {
				// Find or create entry in our accumulator.
				found := false
				for i := range toolCalls {
					if toolCalls[i].ID == tc.ID {
						toolCalls[i].Function.Arguments += tc.Function.Arguments
						found = true
						break
					}
				}
				if !found {
					toolCalls = append(toolCalls, provider.ToolCall{
						ID:   tc.ID,
						Type: tc.Type,
						Function: provider.FunctionCall{
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						},
					})
				}
			}

			// Accumulate usage.
			if chunk.Usage != nil {
				totalUsage.PromptTokens += chunk.Usage.PromptTokens
				totalUsage.CompletionTokens += chunk.Usage.CompletionTokens
				totalUsage.TotalTokens += chunk.Usage.TotalTokens
			}
		}

		_ = stream.SendTextEnd()

		// If no tool calls, we're done.
		if len(toolCalls) == 0 {
			// Persist the assistant message.
			e.persistAssistantMessage(thread.ID, fullContent, totalUsage)
			return totalUsage, nil
		}

		// --- Tool Call Loop ---

		// Emit tool call events and execute each tool.
		var toolResults []provider.Message
		for _, tc := range toolCalls {
			argsBytes := json.RawMessage(tc.Function.Arguments)

			_ = stream.SendToolCallStart(tc.ID, tc.Function.Name, argsBytes)
			_ = stream.SendToolCallEnd(tc.ID)

			// Execute the tool.
			output, execErr := e.tools.Execute(runCtx, tc.Function.Name, tc.Function.Arguments)
			isError := execErr != nil
			if isError {
				output = []byte(fmt.Sprintf(`{"error":"%s"}`, execErr.Error()))
			}

			_ = stream.SendToolOutput(tc.ID, output, isError)

			// Build the tool result message for the next LLM round.
			toolResults = append(toolResults, provider.Message{
				Role:       "tool",
				Content:    string(output),
				ToolCallID: tc.ID,
			})
		}

		// Build the assistant message with tool calls for history.
		assistantToolMsg := provider.Message{
			Role:      "assistant",
			Content:   fullContent,
			ToolCalls: toolCalls,
		}

		// Persist tool call info.
		if fullContent != "" {
			e.persistAssistantMessage(thread.ID, fullContent, totalUsage)
		}

		// Extend messages for the next round.
		messages = append(messages, assistantToolMsg)
		messages = append(messages, toolResults...)
		allMessages = append(allMessages, assistantToolMsg)
		allMessages = append(allMessages, toolResults...)
	}

	// If we exhausted tool rounds, send an error.
	return totalUsage, fmt.Errorf("exceeded maximum tool call rounds (%d)", maxToolRounds)
}

// resolveProvider returns the best available provider for the given agent.
// If the agent has a fallback configured, it returns the fallback provider
// (which internally tries primary → fallback).
func (e *Engine) resolveProvider(agentName string) provider.Provider {
	if fp, ok := e.fallbacks[agentName]; ok {
		return fp
	}
	agentCfg, ok := e.agents[agentName]
	if !ok {
		// Return any provider as a last resort.
		for _, p := range e.providers {
			return p
		}
		return nil
	}
	return e.providers[agentCfg.Provider]
}

// ensureThread returns the existing thread or creates a new one.
func (e *Engine) ensureThread(threadID, agentName string) (*threads.Thread, error) {
	thread, err := e.store.GetThread(threadID)
	if err == nil {
		return thread, nil
	}

	// Thread doesn't exist — create it.
	now := time.Now().UTC()
	thread = &threads.Thread{
		ID:        threadID,
		Agent:     agentName,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := e.store.CreateThread(thread); err != nil {
		return nil, fmt.Errorf("creating thread: %w", err)
	}
	return thread, nil
}

// persistUserMessage saves the user's message to the thread store.
func (e *Engine) persistUserMessage(threadID, content string, state json.RawMessage) error {
	var meta json.RawMessage
	if len(state) > 0 {
		meta = state
	}

	msg := &threads.Message{
		ID:        fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		ThreadID:  threadID,
		Role:      "user",
		Content:   content,
		Metadata:  meta,
		CreatedAt: time.Now().UTC(),
	}
	return e.store.AddMessage(msg)
}

// persistAssistantMessage saves the assistant's response to the thread store.
func (e *Engine) persistAssistantMessage(threadID, content string, usage *agui.TokenUsage) error {
	var meta []byte
	if usage != nil {
		meta, _ = json.Marshal(usage)
	}

	msg := &threads.Message{
		ID:        fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		ThreadID:  threadID,
		Role:      "assistant",
		Content:   content,
		Metadata:  meta,
		CreatedAt: time.Now().UTC(),
	}
	return e.store.AddMessage(msg)
}

// Close releases all resources held by the engine.
func (e *Engine) Close() error {
	var firstErr error
	for _, p := range e.providers {
		if err := p.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if e.store != nil {
		if err := e.store.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// RunID returns the run ID from the stream. Exported for logging.
func RunIDFromStream(s *agui.RunStream) string {
	return s.RunID()
}
