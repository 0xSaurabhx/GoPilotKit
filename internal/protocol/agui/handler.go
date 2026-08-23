package agui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// RunRequest is the incoming request to start an agent run.
type RunRequest struct {
	ThreadID string            `json:"thread_id,omitempty"`
	Agent    string            `json:"agent"`
	Message  string            `json:"message"`
	State    json.RawMessage   `json:"state,omitempty"`
	Meta     map[string]string `json:"meta,omitempty"`
}

// RunFunc is the function signature for agent run handlers.
// It receives a RunRequest and a RunStream to write events to.
type RunFunc func(req RunRequest, stream *RunStream)

// Handler serves the AG-UI SSE streaming protocol.
type Handler struct {
	runFunc RunFunc
	mu      sync.RWMutex
	runs    map[string]*RunState
}

// RunState tracks the state of an active run.
type RunState struct {
	RunID     string
	ThreadID  string
	Agent     string
	StartedAt time.Time
	Status    string // "running", "waiting_interrupt", "completed", "error", "cancelled"
	mu        sync.RWMutex
}

// GetStatus returns the current run status (thread-safe).
func (rs *RunState) GetStatus() string {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	return rs.Status
}

// SetStatus updates the run status (thread-safe).
func (rs *RunState) SetStatus(status string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.Status = status
}

// NewHandler creates a new AG-UI HTTP handler.
func NewHandler(runFunc RunFunc) *Handler {
	return &Handler{
		runFunc: runFunc,
		runs:    make(map[string]*RunState),
	}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/run":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleRun(w, r)
	case "/runs":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleListRuns(w, r)
	case "/health":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleHealth(w, r)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// handleRun starts a new agent run and streams events via SSE.
func (h *Handler) handleRun(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Validate
	if req.Agent == "" {
		http.Error(w, "agent is required", http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	// Generate IDs
	if req.ThreadID == "" {
		req.ThreadID = NewThreadID()
	}
	runID := NewRunID()

	// Track run state
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

	// Clean up on exit
	defer func() {
		state.SetStatus("completed")
		// Keep in map for a while for status queries
		go func() {
			time.Sleep(5 * time.Minute)
			h.mu.Lock()
			delete(h.runs, runID)
			h.mu.Unlock()
		}()
	}()

	// Check for client disconnect
	ctx := r.Context()

	// Create SSE encoder
	encoder, err := NewSSEEncoder(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer encoder.Close()

	// Check context before starting
	if ctx.Err() != nil {
		state.SetStatus("cancelled")
		return
	}

	// Create run stream
	stream := NewRunStream(runID, req.ThreadID, encoder)
	stream.SetContext(ctx)

	// Send RUN_START
	if err := stream.SendRunStart(req.Agent, req.Meta); err != nil {
		log.Printf("AG-UI: failed to send RUN_START: %v", err)
		return
	}

	// Set up heartbeat to detect disconnects
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	go func() {
		for {
			select {
			case <-heartbeat.C:
				if err := encoder.WriteComment("heartbeat"); err != nil {
					return
				}
			case <-ctx.Done():
				stream.Cancel()
				return
			}
		}
	}()

	// Execute the agent run
	h.runFunc(req, stream)

	// Send RUN_END if not already sent
	if !stream.HasEnded() {
		_ = stream.SendRunEnd(nil)
	}
}

// handleListRuns returns active runs.
func (h *Handler) handleListRuns(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	type runInfo struct {
		RunID     string `json:"run_id"`
		ThreadID  string `json:"thread_id"`
		Agent     string `json:"agent"`
		Status    string `json:"status"`
		StartedAt string `json:"started_at"`
	}

	var runs []runInfo
	for _, rs := range h.runs {
		runs = append(runs, runInfo{
			RunID:     rs.RunID,
			ThreadID:  rs.ThreadID,
			Agent:     rs.Agent,
			Status:    rs.GetStatus(),
			StartedAt: rs.StartedAt.Format(time.RFC3339),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"runs": runs,
		"total": len(runs),
	})
}

// handleHealth returns server health status.
func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": "v1alpha",
		"protocol": "ag-ui",
	})
}
