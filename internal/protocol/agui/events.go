package agui

import (
	"encoding/json"
	"time"
)

// EventType represents the type of AG-UI event.
type EventType string

const (
	// Core lifecycle events
	EventRunStart      EventType = "RUN_START"
	EventRunEnd        EventType = "RUN_END"
	EventRunError      EventType = "RUN_ERROR"

	// Text streaming
	EventTextStart     EventType = "TEXT_START"
	EventTextChunk     EventType = "TEXT_CHUNK"
	EventTextEnd       EventType = "TEXT_END"

	// Tool calls
	EventToolCallStart  EventType = "TOOL_CALL_START"
	EventToolCallChunk  EventType = "TOOL_CALL_CHUNK"
	EventToolCallEnd    EventType = "TOOL_CALL_END"
	EventToolOutput     EventType = "TOOL_OUTPUT"

	// Generative UI
	EventGenUIPayload  EventType = "GEN_UI_PAYLOAD"

	// Human-in-the-loop
	EventInterruptRequired EventType = "INTERRUPT_REQUIRED"
	EventInterruptResolved EventType = "INTERRUPT_RESOLVED"

	// State management
	EventStateDelta    EventType = "STATE_DELTA"
	EventStateSnapshot EventType = "STATE_SNAPSHOT"

	// Thinking / reasoning steps
	EventThinkingStart EventType = "THINKING_START"
	EventThinkingChunk EventType = "THINKING_CHUNK"
	EventThinkingEnd   EventType = "THINKING_END"

	// Sub-agents
	EventSubAgentStart EventType = "SUB_AGENT_START"
	EventSubAgentEnd   EventType = "SUB_AGENT_END"

	// Custom events
	EventCustom        EventType = "CUSTOM"

	// Cancellation
	EventRunCancelled  EventType = "RUN_CANCELLED"
)

// Event is the base interface for all AG-UI events.
type Event interface {
	GetType() EventType
	GetRunID() string
	GetTimestamp() time.Time
}

// BaseEvent contains common fields shared by all events.
type BaseEvent struct {
	Type      EventType `json:"type"`
	RunID     string    `json:"run_id"`
	Timestamp time.Time `json:"timestamp"`
}

func (e BaseEvent) GetType() EventType    { return e.Type }
func (e BaseEvent) GetRunID() string      { return e.RunID }
func (e BaseEvent) GetTimestamp() time.Time { return e.Timestamp }

// --- Run Lifecycle Events ---

// RunStartEvent signals the beginning of an agent run.
type RunStartEvent struct {
	BaseEvent
	ThreadID string            `json:"thread_id"`
	Agent    string            `json:"agent"`
	Meta     map[string]string `json:"meta,omitempty"`
}

// RunEndEvent signals the successful completion of an agent run.
type RunEndEvent struct {
	BaseEvent
	TokenUsage *TokenUsage `json:"token_usage,omitempty"`
}

// RunErrorEvent signals a run failure.
type RunErrorEvent struct {
	BaseEvent
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Retryable bool  `json:"retryable"`
}

// --- Text Streaming Events ---

// TextStartEvent signals the beginning of a text response.
type TextStartEvent struct {
	BaseEvent
	MessageID string `json:"message_id,omitempty"`
}

// TextChunkEvent delivers a piece of streamed text.
type TextChunkEvent struct {
	BaseEvent
	Delta string `json:"delta"`
}

// TextEndEvent signals the end of a text response.
type TextEndEvent struct {
	BaseEvent
}

// --- Tool Call Events ---

// ToolCallStartEvent signals a tool invocation.
type ToolCallStartEvent struct {
	BaseEvent
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
}

// ToolCallChunkEvent delivers incremental tool call arguments.
type ToolCallChunkEvent struct {
	BaseEvent
	ToolCallID string `json:"tool_call_id"`
	Delta      string `json:"delta"`
}

// ToolCallEndEvent signals the end of tool argument streaming.
type ToolCallEndEvent struct {
	BaseEvent
	ToolCallID string `json:"tool_call_id"`
}

// ToolOutputEvent delivers the result of a tool invocation.
type ToolOutputEvent struct {
	BaseEvent
	ToolCallID string          `json:"tool_call_id"`
	Output     json.RawMessage `json:"output"`
	IsError    bool            `json:"is_error,omitempty"`
}

// --- Generative UI Events ---

// GenUIPayloadEvent delivers a generative UI component.
type GenUIPayloadEvent struct {
	BaseEvent
	Component string          `json:"component"`
	Props     json.RawMessage `json:"props,omitempty"`
	State     json.RawMessage `json:"state,omitempty"`
}

// --- Human-in-the-Loop Events ---

// InterruptRequiredEvent pauses the run for human input.
type InterruptRequiredEvent struct {
	BaseEvent
	InterruptID string          `json:"interrupt_id"`
	Reason      string          `json:"reason"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

// InterruptResolvedEvent signals that human input has been provided.
type InterruptResolvedEvent struct {
	BaseEvent
	InterruptID string          `json:"interrupt_id"`
	Response    json.RawMessage `json:"response"`
}

// --- State Events ---

// StateDeltaEvent delivers a partial state update.
type StateDeltaEvent struct {
	BaseEvent
	Delta json.RawMessage `json:"delta"`
	Path  string          `json:"path,omitempty"`
}

// StateSnapshotEvent delivers the full state snapshot (e.g., on reconnect).
type StateSnapshotEvent struct {
	BaseEvent
	State json.RawMessage `json:"state"`
}

// --- Thinking Events ---

// ThinkingStartEvent signals the beginning of a reasoning step.
type ThinkingStartEvent struct {
	BaseEvent
}

// ThinkingChunkEvent delivers a piece of reasoning text.
type ThinkingChunkEvent struct {
	BaseEvent
	Delta string `json:"delta"`
}

// ThinkingEndEvent signals the end of a reasoning step.
type ThinkingEndEvent struct {
	BaseEvent
}

// --- Sub-Agent Events ---

// SubAgentStartEvent signals the launch of a sub-agent.
type SubAgentStartEvent struct {
	BaseEvent
	SubAgentRunID string `json:"sub_agent_run_id"`
	AgentName     string `json:"agent_name"`
}

// SubAgentEndEvent signals the completion of a sub-agent.
type SubAgentEndEvent struct {
	BaseEvent
	SubAgentRunID string `json:"sub_agent_run_id"`
}

// --- Custom Events ---

// CustomEvent delivers application-specific data.
type CustomEvent struct {
	BaseEvent
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

// --- Cancellation ---

// RunCancelledEvent signals that a run was cancelled.
type RunCancelledEvent struct {
	BaseEvent
}

// --- Token Usage ---

// TokenUsage tracks token consumption for a run.
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CachedTokens     int `json:"cached_tokens,omitempty"`
}

// --- Event Serialization ---

// MarshalEvent serializes any Event to JSON bytes.
func MarshalEvent(e Event) ([]byte, error) {
	return json.Marshal(e)
}

// MustMarshalEvent is like MarshalEvent but panics on error.
func MustMarshalEvent(e Event) []byte {
	data, err := MarshalEvent(e)
	if err != nil {
		panic("agui: failed to marshal event: " + err.Error())
	}
	return data
}

// NewRunID generates a new run identifier.
func NewRunID() string {
	return "run_" + time.Now().UTC().Format("20060102150405.000000000")
}

// NewThreadID generates a new thread identifier.
func NewThreadID() string {
	return "thread_" + time.Now().UTC().Format("20060102150405.000000000")
}

// Now returns the current time in UTC.
func Now() time.Time {
	return time.Now().UTC()
}
