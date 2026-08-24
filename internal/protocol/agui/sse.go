package agui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// EventEncoder is the interface for writing AG-UI events to a transport.
// Both SSEEncoder (HTTP SSE) and WSEncoder (WebSocket) implement this.
type EventEncoder interface {
	WriteEvent(evt Event) error
	WriteRaw(eventType string, data []byte) error
	WriteComment(comment string) error
	Close()
	IsClosed() bool
}

// SSEEncoder writes AG-UI events in SSE format over an http.ResponseWriter.
//
// SSE format:
//
//	event: RUN_START
//	data: {"type":"RUN_START","run_id":"run_123","timestamp":"..."}
//
// Thread-safe: multiple goroutines may call WriteEvent concurrently.
type SSEEncoder struct {
	mu     sync.Mutex
	writer io.Writer
	flusher http.Flusher
	closed  bool
}

// NewSSEEncoder creates an SSE encoder from an HTTP response writer.
// Returns an error if the response writer doesn't support flushing.
func NewSSEEncoder(w http.ResponseWriter) (*SSEEncoder, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	return &SSEEncoder{
		writer:  w,
		flusher: flusher,
	}, nil
}

// WriteEvent writes a single AG-UI event in SSE format.
func (e *SSEEncoder) WriteEvent(evt Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return fmt.Errorf("encoder is closed")
	}

	data, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshaling event: %w", err)
	}

	// Write SSE frame: event line + data line + blank line
	if _, err := fmt.Fprintf(e.writer, "event: %s\n", evt.GetType()); err != nil {
		return fmt.Errorf("writing event type: %w", err)
	}
	if _, err := fmt.Fprintf(e.writer, "data: %s\n", data); err != nil {
		return fmt.Errorf("writing event data: %w", err)
	}
	if _, err := fmt.Fprint(e.writer, "\n"); err != nil {
		return fmt.Errorf("writing event terminator: %w", err)
	}

	e.flusher.Flush()
	return nil
}

// WriteRaw writes raw SSE data (for custom/non-typed events).
func (e *SSEEncoder) WriteRaw(eventType string, data []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return fmt.Errorf("encoder is closed")
	}

	if _, err := fmt.Fprintf(e.writer, "event: %s\n", eventType); err != nil {
		return fmt.Errorf("writing event type: %w", err)
	}
	if _, err := fmt.Fprintf(e.writer, "data: %s\n", data); err != nil {
		return fmt.Errorf("writing event data: %w", err)
	}
	if _, err := fmt.Fprint(e.writer, "\n"); err != nil {
		return fmt.Errorf("writing event terminator: %w", err)
	}

	e.flusher.Flush()
	return nil
}

// WriteComment writes an SSE comment (lines starting with :).
// Useful for keep-alive heartbeats.
func (e *SSEEncoder) WriteComment(comment string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return fmt.Errorf("encoder is closed")
	}

	if _, err := fmt.Fprintf(e.writer, ": %s\n\n", comment); err != nil {
		return fmt.Errorf("writing comment: %w", err)
	}

	e.flusher.Flush()
	return nil
}

// Close marks the encoder as closed. No further writes will succeed.
func (e *SSEEncoder) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
}

// IsClosed returns whether the encoder has been closed.
func (e *SSEEncoder) IsClosed() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closed
}

// StreamEvents is a convenience function that streams a channel of events
// until the channel is closed or the encoder encounters an error.
func StreamEvents(encoder *SSEEncoder, events <-chan Event) error {
	for evt := range events {
		if err := encoder.WriteEvent(evt); err != nil {
			return err
		}
	}
	return nil
}
