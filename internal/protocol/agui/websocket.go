package agui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSUpgrader configures the WebSocket connection upgrade.
var WSUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true }, // Allow all origins for now.
}

// WSEncoder writes AG-UI events as JSON messages over a WebSocket connection.
// Thread-safe: multiple goroutines may call WriteEvent concurrently.
type WSEncoder struct {
	mu     sync.Mutex
	conn   *websocket.Conn
	closed bool
}

// NewWSEncoder creates a WebSocket encoder from an upgraded connection.
func NewWSEncoder(conn *websocket.Conn) *WSEncoder {
	return &WSEncoder{conn: conn}
}

// wsEvent wraps an AG-UI event for JSON transmission over WebSocket.
type wsEvent struct {
	Type      EventType     `json:"type"`
	RunID     string        `json:"run_id"`
	Timestamp time.Time     `json:"timestamp"`
	Payload   interface{} `json:"payload,omitempty"`
}

// WriteEvent writes a single AG-UI event as a JSON message over WebSocket.
func (e *WSEncoder) WriteEvent(evt Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return fmt.Errorf("encoder is closed")
	}

	// Serialize the full event to JSON.
	data, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshaling event: %w", err)
	}

	return e.conn.WriteMessage(websocket.TextMessage, data)
}

// WriteRaw writes raw JSON data as a WebSocket text message.
func (e *WSEncoder) WriteRaw(eventType string, data []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return fmt.Errorf("encoder is closed")
	}

	// Wrap in an envelope with the event type.
	envelope := map[string]interface{}{
		"type": eventType,
		"data": json.RawMessage(data),
	}
	envelopeData, err := json.Marshal(envelope)
	if err != nil {
		return err
	}

	return e.conn.WriteMessage(websocket.TextMessage, envelopeData)
}

// WriteComment writes an SSE-style comment. Over WebSocket this is a no-op
// since WebSocket has its own ping/pong keepalive mechanism.
func (e *WSEncoder) WriteComment(comment string) error {
	// WebSocket has built-in ping/pong; comments are not needed.
	return nil
}

// Close marks the encoder as closed and closes the WebSocket connection.
func (e *WSEncoder) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.closed {
		e.closed = true
		e.conn.Close()
	}
}

// IsClosed returns whether the encoder has been closed.
func (e *WSEncoder) IsClosed() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closed
}

// handleWebSocket handles the /ws endpoint — upgrades to WebSocket,
// reads RunRequest messages, and streams AG-UI events back.
func (h *Handler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := WSUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("AG-UI WS: upgrade failed: %v", err)
		return
	}

	// Set up pong handler for keepalive.
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Start ping goroutine for keepalive.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()

	// Read messages from the client.
	for {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("AG-UI WS: client disconnected")
			} else {
				log.Printf("AG-UI WS: read error: %v", err)
			}
			return
		}

		var req RunRequest
		if err := json.Unmarshal(message, &req); err != nil {
			h.wsSendError(conn, fmt.Sprintf("invalid request: %v", err))
			continue
		}

		if req.Agent == "" {
			h.wsSendError(conn, "agent is required")
			continue
		}
		if req.Message == "" {
			h.wsSendError(conn, "message is required")
			continue
		}

		// Generate IDs.
		if req.ThreadID == "" {
			req.ThreadID = NewThreadID()
		}
		runID := NewRunID()

		// Track run state.
		state := &RunState{
			RunID:     runID,
			ThreadID:  req.ThreadID,
			Agent:     req.Agent,
			StartedAt: Now(),
			Status:    "running",
		}
		h.mu.Lock()
		h.runs[runID] = state
		h.mu.Unlock()

		// Clean up on exit.
		defer func(id string) {
			state.SetStatus("completed")
			go func() {
				time.Sleep(5 * time.Minute)
				h.mu.Lock()
				delete(h.runs, id)
				h.mu.Unlock()
			}()
		}(runID)

		// Create encoder and stream.
		encoder := NewWSEncoder(conn)
		ctx := r.Context()
		stream := NewRunStream(runID, req.ThreadID, encoder)
		stream.SetContext(ctx)

		// Send RUN_START.
		if err := stream.SendRunStart(req.Agent, req.Meta); err != nil {
			log.Printf("AG-UI WS: failed to send RUN_START: %v", err)
			return
		}

		// Execute the agent run.
		h.runFunc(req, stream)

		// Send RUN_END if not already sent.
		if !stream.HasEnded() {
			_ = stream.SendRunEnd(nil)
		}
	}
}

// wsSendError sends a JSON error message directly over a WebSocket connection.
func (h *Handler) wsSendError(conn *websocket.Conn, errMsg string) {
	errEvent := map[string]interface{}{
		"type":  "RUN_ERROR",
		"error": errMsg,
		"code":  "INVALID_REQUEST",
	}
	data, _ := json.Marshal(errEvent)
	conn.WriteMessage(websocket.TextMessage, data)
}
