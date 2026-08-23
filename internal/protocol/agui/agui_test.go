package agui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- Event Tests ---

func TestEventTypes(t *testing.T) {
	events := []struct {
		event Event
	_type  EventType
	}{
		{&RunStartEvent{BaseEvent: BaseEvent{Type: EventRunStart}}, EventRunStart},
		{&RunEndEvent{BaseEvent: BaseEvent{Type: EventRunEnd}}, EventRunEnd},
		{&TextChunkEvent{BaseEvent: BaseEvent{Type: EventTextChunk}}, EventTextChunk},
		{&ToolCallStartEvent{BaseEvent: BaseEvent{Type: EventToolCallStart}}, EventToolCallStart},
		{&GenUIPayloadEvent{BaseEvent: BaseEvent{Type: EventGenUIPayload}}, EventGenUIPayload},
		{&InterruptRequiredEvent{BaseEvent: BaseEvent{Type: EventInterruptRequired}}, EventInterruptRequired},
		{&StateDeltaEvent{BaseEvent: BaseEvent{Type: EventStateDelta}}, EventStateDelta},
		{&ThinkingChunkEvent{BaseEvent: BaseEvent{Type: EventThinkingChunk}}, EventThinkingChunk},
		{&SubAgentStartEvent{BaseEvent: BaseEvent{Type: EventSubAgentStart}}, EventSubAgentStart},
		{&CustomEvent{BaseEvent: BaseEvent{Type: EventCustom}}, EventCustom},
	}

	for _, tc := range events {
		if tc.event.GetType() != tc._type {
			t.Errorf("expected type %s, got %s", tc._type, tc.event.GetType())
		}
		if tc.event.GetRunID() != "" {
			t.Error("expected empty run ID on base event")
		}
	}
}

func TestBaseEventFields(t *testing.T) {
	now := time.Now().UTC()
	evt := BaseEvent{
		Type:      EventTextChunk,
		RunID:     "run_123",
		Timestamp: now,
	}

	if evt.GetType() != EventTextChunk {
		t.Errorf("expected TEXT_CHUNK, got %s", evt.GetType())
	}
	if evt.GetRunID() != "run_123" {
		t.Errorf("expected run_123, got %s", evt.GetRunID())
	}
	if !evt.GetTimestamp().Equal(now) {
		t.Errorf("expected timestamp %v, got %v", now, evt.GetTimestamp())
	}
}

func TestMarshalEvent(t *testing.T) {
	evt := &TextChunkEvent{
		BaseEvent: BaseEvent{
			Type:      EventTextChunk,
			RunID:     "run_test",
			Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		Delta: "hello world",
	}

	data, err := MarshalEvent(evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse marshaled event: %v", err)
	}

	if parsed["type"] != "TEXT_CHUNK" {
		t.Errorf("expected type TEXT_CHUNK, got %v", parsed["type"])
	}
	if parsed["delta"] != "hello world" {
		t.Errorf("expected delta 'hello world', got %v", parsed["delta"])
	}
	if parsed["run_id"] != "run_test" {
		t.Errorf("expected run_id 'run_test', got %v", parsed["run_id"])
	}
}

func TestNewRunID(t *testing.T) {
	id1 := NewRunID()
	id2 := NewRunID()

	if !strings.HasPrefix(id1, "run_") {
		t.Errorf("expected run_ prefix, got %s", id1)
	}
	if id1 == id2 {
		t.Error("expected unique run IDs")
	}
}

func TestNewThreadID(t *testing.T) {
	id1 := NewThreadID()
	id2 := NewThreadID()

	if !strings.HasPrefix(id1, "thread_") {
		t.Errorf("expected thread_ prefix, got %s", id1)
	}
	if id1 == id2 {
		t.Error("expected unique thread IDs")
	}
}

// --- SSE Encoder Tests ---

func TestSSEEncoderWriteEvent(t *testing.T) {
	w := httptest.NewRecorder()
	encoder, err := NewSSEEncoder(w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	evt := &TextChunkEvent{
		BaseEvent: BaseEvent{
			Type:      EventTextChunk,
			RunID:     "run_test",
			Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		Delta: "hello",
	}

	if err := encoder.WriteEvent(evt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "event: TEXT_CHUNK") {
		t.Errorf("expected 'event: TEXT_CHUNK' in output, got:\n%s", body)
	}
	if !strings.Contains(body, "data: ") {
		t.Errorf("expected 'data: ' in output, got:\n%s", body)
	}
	if !strings.Contains(body, `"delta":"hello"`) {
		t.Errorf("expected delta in output, got:\n%s", body)
	}
}

func TestSSEEncoderWriteComment(t *testing.T) {
	w := httptest.NewRecorder()
	encoder, err := NewSSEEncoder(w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := encoder.WriteComment("heartbeat"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, ": heartbeat") {
		t.Errorf("expected ': heartbeat' in output, got:\n%s", body)
	}
}

func TestSSEEncoderWriteRaw(t *testing.T) {
	w := httptest.NewRecorder()
	encoder, err := NewSSEEncoder(w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := encoder.WriteRaw("CUSTOM_EVENT", []byte(`{"key":"value"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "event: CUSTOM_EVENT") {
		t.Errorf("expected 'event: CUSTOM_EVENT' in output, got:\n%s", body)
	}
}

func TestSSEEncoderClose(t *testing.T) {
	w := httptest.NewRecorder()
	encoder, err := NewSSEEncoder(w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if encoder.IsClosed() {
		t.Error("expected encoder to be open")
	}

	encoder.Close()

	if !encoder.IsClosed() {
		t.Error("expected encoder to be closed")
	}

	evt := &TextChunkEvent{
		BaseEvent: BaseEvent{Type: EventTextChunk, RunID: "test", Timestamp: Now()},
		Delta:     "test",
	}
	err = encoder.WriteEvent(evt)
	if err == nil {
		t.Error("expected error writing to closed encoder")
	}
}

func TestSSEEncoderContentHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	encoder, err := NewSSEEncoder(w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = encoder

	ct := w.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("expected Content-Type 'text/event-stream', got '%s'", ct)
	}

	cc := w.Header().Get("Cache-Control")
	if !strings.Contains(cc, "no-cache") {
		t.Errorf("expected Cache-Control to contain 'no-cache', got '%s'", cc)
	}
}

// --- HTTP Handler Tests ---

func TestHandlerHealthEndpoint(t *testing.T) {
	handler := NewHandler(func(req RunRequest, stream *RunStream) {})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got '%s'", resp["status"])
	}
}

func TestHandlerListRunsEndpoint(t *testing.T) {
	handler := NewHandler(func(req RunRequest, stream *RunStream) {})
	req := httptest.NewRequest(http.MethodGet, "/runs", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["total"].(float64) != 0 {
		t.Errorf("expected 0 runs, got %v", resp["total"])
	}
}

func TestHandlerRunMissingAgent(t *testing.T) {
	handler := NewHandler(func(req RunRequest, stream *RunStream) {})

	body := `{"message": "hello"}`
	req := httptest.NewRequest(http.MethodPost, "/run", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandlerRunMissingMessage(t *testing.T) {
	handler := NewHandler(func(req RunRequest, stream *RunStream) {})

	body := `{"agent": "echo"}`
	req := httptest.NewRequest(http.MethodPost, "/run", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandlerRunInvalidBody(t *testing.T) {
	handler := NewHandler(func(req RunRequest, stream *RunStream) {})

	req := httptest.NewRequest(http.MethodPost, "/run", bytes.NewBufferString("not json"))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandlerNotFound(t *testing.T) {
	handler := NewHandler(func(req RunRequest, stream *RunStream) {})

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestHandlerMethodNotAllowed(t *testing.T) {
	handler := NewHandler(func(req RunRequest, stream *RunStream) {})

	req := httptest.NewRequest(http.MethodGet, "/run", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

// --- Coordinator Tests ---

func TestCoordinatorRegisterAndGetHandler(t *testing.T) {
	coord := NewCoordinator()

	called := false
	coord.RegisterHandler("test-agent", func(req RunRequest, stream *RunStream) {
		called = true
	})

	handler, ok := coord.GetHandler("test-agent")
	if !ok {
		t.Fatal("expected handler to be found")
	}

	// Invoke handler
	w := httptest.NewRecorder()
	encoder, _ := NewSSEEncoder(w)
	stream := NewRunStream("run_test", "thread_test", encoder)

	handler(RunRequest{Agent: "test-agent", Message: "hello"}, stream)

	if !called {
		t.Error("expected handler to be called")
	}
}

func TestCoordinatorGetMissingHandler(t *testing.T) {
	coord := NewCoordinator()

	_, ok := coord.GetHandler("nonexistent")
	if ok {
		t.Error("expected handler to not be found")
	}
}

func TestCoordinatorExecuteRunMissingAgent(t *testing.T) {
	coord := NewCoordinator()

	w := httptest.NewRecorder()
	encoder, _ := NewSSEEncoder(w)
	stream := NewRunStream("run_test", "thread_test", encoder)

	_ = stream.SendRunStart("echo", nil)

	coord.ExecuteRun(t.Context(), RunRequest{Agent: "missing", Message: "hello"}, stream)

	body := w.Body.String()
	if !strings.Contains(body, "RUN_ERROR") {
		t.Errorf("expected RUN_ERROR in output, got:\n%s", body)
	}
	if !strings.Contains(body, "not found") {
		t.Errorf("expected 'not found' in error, got:\n%s", body)
	}
}

func TestCoordinatorExecuteRunPanickingHandler(t *testing.T) {
	coord := NewCoordinator()
	coord.RegisterHandler("panic-agent", func(req RunRequest, stream *RunStream) {
		panic("something went wrong")
	})

	w := httptest.NewRecorder()
	encoder, _ := NewSSEEncoder(w)
	stream := NewRunStream("run_test", "thread_test", encoder)

	_ = stream.SendRunStart("panic-agent", nil)

	coord.ExecuteRun(t.Context(), RunRequest{Agent: "panic-agent", Message: "hello"}, stream)

	body := w.Body.String()
	if !strings.Contains(body, "RUN_ERROR") {
		t.Errorf("expected RUN_ERROR in output, got:\n%s", body)
	}
	if !strings.Contains(body, "panic") {
		t.Errorf("expected 'panic' in error, got:\n%s", body)
	}
}

func TestDefaultCoordinatorEchoHandler(t *testing.T) {
	coord := DefaultCoordinator()

	w := httptest.NewRecorder()
	encoder, _ := NewSSEEncoder(w)
	stream := NewRunStream("run_test", "thread_test", encoder)

	_ = stream.SendRunStart("echo", nil)

	handler, ok := coord.GetHandler("echo")
	if !ok {
		t.Fatal("echo handler not found")
	}

	handler(RunRequest{Agent: "echo", Message: "hello world"}, stream)

	body := w.Body.String()
	if !strings.Contains(body, "Echo: hello world") {
		t.Errorf("expected 'Echo: hello world' in output, got:\n%s", body)
	}
}

// --- Stream Tests ---

func TestRunStreamHasEnded(t *testing.T) {
	w := httptest.NewRecorder()
	encoder, _ := NewSSEEncoder(w)
	stream := NewRunStream("run_test", "thread_test", encoder)

	if stream.HasEnded() {
		t.Error("expected stream to not be ended")
	}

	_ = stream.SendRunEnd(nil)

	if !stream.HasEnded() {
		t.Error("expected stream to be ended")
	}
}

func TestRunStreamCancel(t *testing.T) {
	w := httptest.NewRecorder()
	encoder, _ := NewSSEEncoder(w)
	stream := NewRunStream("run_test", "thread_test", encoder)

	select {
	case <-stream.Cancelled():
		t.Error("expected cancel channel to be open")
	default:
	}

	stream.Cancel()

	select {
	case <-stream.Cancelled():
		// Expected
	case <-time.After(time.Second):
		t.Error("expected cancel channel to be closed")
	}
}

func TestStreamEvents(t *testing.T) {
	w := httptest.NewRecorder()
	encoder, _ := NewSSEEncoder(w)

	events := make(chan Event, 3)
	events <- &TextChunkEvent{
		BaseEvent: BaseEvent{Type: EventTextChunk, RunID: "r1", Timestamp: Now()},
		Delta:     "hello",
	}
	events <- &TextChunkEvent{
		BaseEvent: BaseEvent{Type: EventTextChunk, RunID: "r1", Timestamp: Now()},
		Delta:     "world",
	}
	close(events)

	err := StreamEvents(encoder, events)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := w.Body.String()
	if strings.Count(body, "event: TEXT_CHUNK") != 2 {
		t.Errorf("expected 2 TEXT_CHUNK events, got:\n%s", body)
	}
}

// --- InterruptResponse Tests ---

func TestInterruptResponseMarshal(t *testing.T) {
	resp := &InterruptResponse{
		InterruptID: "int_123",
		Approved:    true,
		Response:    json.RawMessage(`{"reason":"looks good"}`),
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed InterruptResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.InterruptID != "int_123" {
		t.Errorf("expected int_123, got %s", parsed.InterruptID)
	}
	if !parsed.Approved {
		t.Error("expected approved to be true")
	}
}
