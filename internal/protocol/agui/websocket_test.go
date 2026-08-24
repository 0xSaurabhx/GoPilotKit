package agui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocketEcho(t *testing.T) {
	// Set up a handler with the echo agent.
	coord := DefaultCoordinator()
	handler := NewHandler(func(req RunRequest, stream *RunStream) {
		coord.ExecuteRun(t.Context(), req, stream)
	})

	// Start a test HTTP server.
	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	// Connect via WebSocket.
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer ws.Close()

	// Send a RunRequest.
	req := RunRequest{
		Agent:   "echo",
		Message: "Hello, WebSocket!",
	}
	reqBytes, _ := json.Marshal(req)
	if err := ws.WriteMessage(websocket.TextMessage, reqBytes); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}

	// Read events until we get RUN_END or RUN_ERROR.
	var events []map[string]interface{}
	deadline := time.After(5 * time.Second)
	done := false

	for !done {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for events")
		default:
		}

		ws.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, message, err := ws.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				break
			}
			t.Fatalf("read error: %v", err)
		}

		var event map[string]interface{}
		if err := json.Unmarshal(message, &event); err != nil {
			t.Fatalf("failed to parse event: %v", err)
		}
		events = append(events, event)

		eventType, _ := event["type"].(string)
		if eventType == "RUN_END" || eventType == "RUN_ERROR" {
			done = true
		}
	}

	// Verify we got the expected events.
	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}

	// Check for RUN_START.
	foundRunStart := false
	for _, e := range events {
		if t, ok := e["type"].(string); ok && t == "RUN_START" {
			foundRunStart = true
			break
		}
	}
	if !foundRunStart {
		t.Error("expected RUN_START event")
	}

	// Check for TEXT_CHUNK with the echoed message.
	foundEcho := false
	for _, e := range events {
		if t, ok := e["type"].(string); ok && t == "TEXT_CHUNK" {
			if delta, ok := e["delta"].(string); ok && strings.Contains(delta, "Echo: Hello, WebSocket!") {
				foundEcho = true
				break
			}
		}
	}
	if !foundEcho {
		t.Error("expected TEXT_CHUNK containing echoed message")
	}

	// Check for RUN_END.
	foundRunEnd := false
	for _, e := range events {
		if t, ok := e["type"].(string); ok && t == "RUN_END" {
			foundRunEnd = true
			break
		}
	}
	if !foundRunEnd {
		t.Error("expected RUN_END event")
	}
}

func TestWebSocketValidation(t *testing.T) {
	coord := DefaultCoordinator()
	handler := NewHandler(func(req RunRequest, stream *RunStream) {
		coord.ExecuteRun(t.Context(), req, stream)
	})

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer ws.Close()

	// Send a request with missing agent.
	req := RunRequest{Message: "Hello"}
	reqBytes, _ := json.Marshal(req)
	ws.WriteMessage(websocket.TextMessage, reqBytes)

	// Read the error response.
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, message, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read error: %v", err)
	}

	var event map[string]interface{}
	json.Unmarshal(message, &event)

	if eventType, ok := event["type"].(string); ok && eventType != "RUN_ERROR" {
		t.Errorf("expected RUN_ERROR, got %s", eventType)
	}
}

func TestWebSocketToolDemo(t *testing.T) {
	coord := DefaultCoordinator()
	handler := NewHandler(func(req RunRequest, stream *RunStream) {
		coord.ExecuteRun(t.Context(), req, stream)
	})

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer ws.Close()

	// Send a tool-demo request.
	req := RunRequest{Agent: "tool-demo", Message: "search for go patterns"}
	reqBytes, _ := json.Marshal(req)
	ws.WriteMessage(websocket.TextMessage, reqBytes)

	// Read events until RUN_END.
	var events []map[string]interface{}
	deadline := time.After(5 * time.Second)
	done := false

	for !done {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for events")
		default:
		}

		ws.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, message, err := ws.ReadMessage()
		if err != nil {
			break
		}

		var event map[string]interface{}
		json.Unmarshal(message, &event)
		events = append(events, event)

		eventType, _ := event["type"].(string)
		if eventType == "RUN_END" || eventType == "RUN_ERROR" {
			done = true
		}
	}

	// Verify tool call events.
	foundToolStart := false
	foundToolEnd := false
	foundToolOutput := false
	for _, e := range events {
		t, _ := e["type"].(string)
		switch t {
		case "TOOL_CALL_START":
			foundToolStart = true
		case "TOOL_CALL_END":
			foundToolEnd = true
		case "TOOL_OUTPUT":
			foundToolOutput = true
		}
	}

	if !foundToolStart {
		t.Error("expected TOOL_CALL_START event")
	}
	if !foundToolEnd {
		t.Error("expected TOOL_CALL_END event")
	}
	if !foundToolOutput {
		t.Error("expected TOOL_OUTPUT event")
	}
}
