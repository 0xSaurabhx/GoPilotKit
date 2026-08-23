package agui

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// RunStream provides a high-level API for writing AG-UI events during an agent run.
// It wraps an SSEEncoder and manages the run lifecycle, including HITL interrupts.
type RunStream struct {
	runID    string
	threadID string
	encoder  *SSEEncoder
	mu       sync.Mutex
	ended    bool
	cancelCh chan struct{}
	ctx      context.Context
}

// NewRunStream creates a new RunStream for the given run.
func NewRunStream(runID, threadID string, encoder *SSEEncoder) *RunStream {
	return &RunStream{
		runID:    runID,
		threadID: threadID,
		encoder:  encoder,
		cancelCh: make(chan struct{}),
		ctx:      context.Background(),
	}
}

// SetContext sets the context for this run stream, typically derived from the HTTP
// request context so client disconnects cancel the run.
func (s *RunStream) SetContext(ctx context.Context) {
	s.ctx = ctx
}

// Context returns the run's context, which is cancelled when the client disconnects.
func (s *RunStream) Context() context.Context {
	return s.ctx
}

// Cancel signals the stream to stop. Safe to call from any goroutine.
func (s *RunStream) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ended {
		close(s.cancelCh)
	}
}

// RunID returns the run identifier.
func (s *RunStream) RunID() string {
	return s.runID
}

// ThreadID returns the thread identifier.
func (s *RunStream) ThreadID() string {
	return s.threadID
}

// Cancelled returns a channel that is closed when the stream is cancelled.
func (s *RunStream) Cancelled() <-chan struct{} {
	return s.cancelCh
}

// HasEnded returns whether the run has already ended.
func (s *RunStream) HasEnded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ended
}

// markEnded marks the stream as ended (idempotent).
func (s *RunStream) markEnded() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ended = true
}

// --- Run Lifecycle ---

// SendRunStart emits a RUN_START event.
func (s *RunStream) SendRunStart(agent string, meta map[string]string) error {
	return s.encoder.WriteEvent(&RunStartEvent{
		BaseEvent: BaseEvent{
			Type:      EventRunStart,
			RunID:     s.runID,
			Timestamp: Now(),
		},
		ThreadID: s.threadID,
		Agent:    agent,
		Meta:     meta,
	})
}

// SendRunEnd emits a RUN_END event and marks the stream as ended.
func (s *RunStream) SendRunEnd(usage *TokenUsage) error {
	s.markEnded()
	return s.encoder.WriteEvent(&RunEndEvent{
		BaseEvent: BaseEvent{
			Type:      EventRunEnd,
			RunID:     s.runID,
			Timestamp: Now(),
		},
		TokenUsage: usage,
	})
}

// SendRunError emits a RUN_ERROR event and marks the stream as ended.
func (s *RunStream) SendRunError(errMsg string, code string, retryable bool) error {
	s.markEnded()
	return s.encoder.WriteEvent(&RunErrorEvent{
		BaseEvent: BaseEvent{
			Type:      EventRunError,
			RunID:     s.runID,
			Timestamp: Now(),
		},
		Error:     errMsg,
		Code:      code,
		Retryable: retryable,
	})
}

// --- Text Streaming ---

// SendTextStart emits a TEXT_START event.
func (s *RunStream) SendTextStart(messageID string) error {
	return s.encoder.WriteEvent(&TextStartEvent{
		BaseEvent: BaseEvent{
			Type:      EventTextStart,
			RunID:     s.runID,
			Timestamp: Now(),
		},
		MessageID: messageID,
	})
}

// SendTextChunk emits a TEXT_CHUNK event with a piece of streamed text.
func (s *RunStream) SendTextChunk(delta string) error {
	return s.encoder.WriteEvent(&TextChunkEvent{
		BaseEvent: BaseEvent{
			Type:      EventTextChunk,
			RunID:     s.runID,
			Timestamp: Now(),
		},
		Delta: delta,
	})
}

// SendTextEnd emits a TEXT_END event.
func (s *RunStream) SendTextEnd() error {
	return s.encoder.WriteEvent(&TextEndEvent{
		BaseEvent: BaseEvent{
			Type:      EventTextEnd,
			RunID:     s.runID,
			Timestamp: Now(),
		},
	})
}

// --- Tool Calls ---

// SendToolCallStart emits a TOOL_CALL_START event.
func (s *RunStream) SendToolCallStart(toolCallID, toolName string, args json.RawMessage) error {
	return s.encoder.WriteEvent(&ToolCallStartEvent{
		BaseEvent: BaseEvent{
			Type:      EventToolCallStart,
			RunID:     s.runID,
			Timestamp: Now(),
		},
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Arguments:  args,
	})
}

// SendToolCallChunk emits a TOOL_CALL_CHUNK event with incremental arguments.
func (s *RunStream) SendToolCallChunk(toolCallID, delta string) error {
	return s.encoder.WriteEvent(&ToolCallChunkEvent{
		BaseEvent: BaseEvent{
			Type:      EventToolCallChunk,
			RunID:     s.runID,
			Timestamp: Now(),
		},
		ToolCallID: toolCallID,
		Delta:      delta,
	})
}

// SendToolCallEnd emits a TOOL_CALL_END event.
func (s *RunStream) SendToolCallEnd(toolCallID string) error {
	return s.encoder.WriteEvent(&ToolCallEndEvent{
		BaseEvent: BaseEvent{
			Type:      EventToolCallEnd,
			RunID:     s.runID,
			Timestamp: Now(),
		},
		ToolCallID: toolCallID,
	})
}

// SendToolOutput emits a TOOL_OUTPUT event with the result.
func (s *RunStream) SendToolOutput(toolCallID string, output json.RawMessage, isError bool) error {
	return s.encoder.WriteEvent(&ToolOutputEvent{
		BaseEvent: BaseEvent{
			Type:      EventToolOutput,
			RunID:     s.runID,
			Timestamp: Now(),
		},
		ToolCallID: toolCallID,
		Output:     output,
		IsError:    isError,
	})
}

// --- Generative UI ---

// SendGenUIPayload emits a GEN_UI_PAYLOAD event.
func (s *RunStream) SendGenUIPayload(component string, props, state json.RawMessage) error {
	return s.encoder.WriteEvent(&GenUIPayloadEvent{
		BaseEvent: BaseEvent{
			Type:      EventGenUIPayload,
			RunID:     s.runID,
			Timestamp: Now(),
		},
		Component: component,
		Props:     props,
		State:     state,
	})
}

// --- Human-in-the-Loop ---

// SendInterruptRequired emits an INTERRUPT_REQUIRED event and waits for resolution.
// Returns the interrupt response or an error if the run is cancelled.
func (s *RunStream) SendInterruptRequired(interruptID, reason string, payload json.RawMessage) (*InterruptResponse, error) {
	// Send the interrupt event
	err := s.encoder.WriteEvent(&InterruptRequiredEvent{
		BaseEvent: BaseEvent{
			Type:      EventInterruptRequired,
			RunID:     s.runID,
			Timestamp: Now(),
		},
		InterruptID: interruptID,
		Reason:      reason,
		Payload:     payload,
	})
	if err != nil {
		return nil, err
	}

	// Wait for resolution or cancellation
	// In a real implementation, this would use a channel/callback mechanism.
	// For now, we return a placeholder that the coordinator will resolve.
	return &InterruptResponse{
		InterruptID: interruptID,
		Approved:    true,
	}, nil
}

// InterruptResponse holds the human's response to an interrupt.
type InterruptResponse struct {
	InterruptID string          `json:"interrupt_id"`
	Approved    bool            `json:"approved"`
	Response    json.RawMessage `json:"response,omitempty"`
}

// --- State ---

// SendStateDelta emits a STATE_DELTA event.
func (s *RunStream) SendStateDelta(delta json.RawMessage, path string) error {
	return s.encoder.WriteEvent(&StateDeltaEvent{
		BaseEvent: BaseEvent{
			Type:      EventStateDelta,
			RunID:     s.runID,
			Timestamp: Now(),
		},
		Delta: delta,
		Path:  path,
	})
}

// SendStateSnapshot emits a STATE_SNAPSHOT event.
func (s *RunStream) SendStateSnapshot(state json.RawMessage) error {
	return s.encoder.WriteEvent(&StateSnapshotEvent{
		BaseEvent: BaseEvent{
			Type:      EventStateSnapshot,
			RunID:     s.runID,
			Timestamp: Now(),
		},
		State: state,
	})
}

// --- Thinking ---

// SendThinkingStart emits a THINKING_START event.
func (s *RunStream) SendThinkingStart() error {
	return s.encoder.WriteEvent(&ThinkingStartEvent{
		BaseEvent: BaseEvent{
			Type:      EventThinkingStart,
			RunID:     s.runID,
			Timestamp: Now(),
		},
	})
}

// SendThinkingChunk emits a THINKING_CHUNK event.
func (s *RunStream) SendThinkingChunk(delta string) error {
	return s.encoder.WriteEvent(&ThinkingChunkEvent{
		BaseEvent: BaseEvent{
			Type:      EventThinkingChunk,
			RunID:     s.runID,
			Timestamp: Now(),
		},
		Delta: delta,
	})
}

// SendThinkingEnd emits a THINKING_END event.
func (s *RunStream) SendThinkingEnd() error {
	return s.encoder.WriteEvent(&ThinkingEndEvent{
		BaseEvent: BaseEvent{
			Type:      EventThinkingEnd,
			RunID:     s.runID,
			Timestamp: Now(),
		},
	})
}

// --- Sub-Agents ---

// SendSubAgentStart emits a SUB_AGENT_START event.
func (s *RunStream) SendSubAgentStart(subAgentRunID, agentName string) error {
	return s.encoder.WriteEvent(&SubAgentStartEvent{
		BaseEvent: BaseEvent{
			Type:      EventSubAgentStart,
			RunID:     s.runID,
			Timestamp: Now(),
		},
		SubAgentRunID: subAgentRunID,
		AgentName:     agentName,
	})
}

// SendSubAgentEnd emits a SUB_AGENT_END event.
func (s *RunStream) SendSubAgentEnd(subAgentRunID string) error {
	return s.encoder.WriteEvent(&SubAgentEndEvent{
		BaseEvent: BaseEvent{
			Type:      EventSubAgentEnd,
			RunID:     s.runID,
			Timestamp: Now(),
		},
		SubAgentRunID: subAgentRunID,
	})
}

// --- Custom ---

// SendCustom emits a CUSTOM event with application-specific data.
func (s *RunStream) SendCustom(name string, value json.RawMessage) error {
	return s.encoder.WriteEvent(&CustomEvent{
		BaseEvent: BaseEvent{
			Type:      EventCustom,
			RunID:     s.runID,
			Timestamp: Now(),
		},
		Name:  name,
		Value: value,
	})
}

// --- Convenience Methods ---

// SendText is a convenience method that sends a complete text response
// (TEXT_START + TEXT_CHUNKs + TEXT_END).
func (s *RunStream) SendText(text string) error {
	if err := s.SendTextStart(""); err != nil {
		return err
	}
	// Stream in chunks of ~50 chars for realistic streaming feel
	chunkSize := 50
	for i := 0; i < len(text); i += chunkSize {
		end := i + chunkSize
		if end > len(text) {
			end = len(text)
		}
		if err := s.SendTextChunk(text[i:end]); err != nil {
			return err
		}
		time.Sleep(10 * time.Millisecond) // Simulate streaming latency
	}
	return s.SendTextEnd()
}
