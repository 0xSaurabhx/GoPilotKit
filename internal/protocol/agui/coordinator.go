package agui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// Coordinator manages the execution of agent runs, including HITL interrupts,
// context propagation, and graceful cancellation.
type Coordinator struct {
	handlers map[string]RunFunc
	mu       sync.RWMutex
}

// NewCoordinator creates a new run coordinator.
func NewCoordinator() *Coordinator {
	return &Coordinator{
		handlers: make(map[string]RunFunc),
	}
}

// RegisterHandler registers an agent run handler for the given agent name.
func (c *Coordinator) RegisterHandler(agent string, fn RunFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[agent] = fn
}

// GetHandler returns the registered handler for the given agent name.
func (c *Coordinator) GetHandler(agent string) (RunFunc, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	fn, ok := c.handlers[agent]
	return fn, ok
}

// ExecuteRun runs the agent handler within a controlled context.
// It propagates cancellation, tracks timing, and handles errors.
func (c *Coordinator) ExecuteRun(ctx context.Context, req RunRequest, stream *RunStream) {
	start := time.Now()

	// Create a child context with cancellation
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Monitor for client disconnect
	go func() {
		select {
		case <-runCtx.Done():
			return
		case <-stream.Cancelled():
			cancel()
			_ = stream.SendRunError("run cancelled by client", "CANCELLED", false)
		}
	}()

	// Find handler
	handler, ok := c.GetHandler(req.Agent)
	if !ok {
		_ = stream.SendRunError(
			fmt.Sprintf("agent %q not found", req.Agent),
			"AGENT_NOT_FOUND",
			false,
		)
		return
	}

	// Execute the handler
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("AG-UI: panic in agent handler %q: %v", req.Agent, r)
				_ = stream.SendRunError(
					fmt.Sprintf("agent panic: %v", r),
					"PANIC",
					true,
				)
			}
		}()

		// Run the handler
		handler(req, stream)
	}()

	elapsed := time.Since(start)
	log.Printf("AG-UI: run %s completed in %v", stream.runID, elapsed)
}

// DefaultCoordinator creates a coordinator with a simple echo handler for testing.
func DefaultCoordinator() *Coordinator {
	coord := NewCoordinator()

	// Register a demo echo handler
	coord.RegisterHandler("echo", func(req RunRequest, stream *RunStream) {
		_ = stream.SendTextStart("")
		_ = stream.SendTextChunk(fmt.Sprintf("Echo: %s", req.Message))
		_ = stream.SendTextEnd()
	})

	// Register a demo handler that simulates tool use
	coord.RegisterHandler("tool-demo", func(req RunRequest, stream *RunStream) {
		_ = stream.SendTextStart("")
		_ = stream.SendTextChunk("I'll look that up for you...\n\n")

		// Simulate tool call
		args, _ := json.Marshal(map[string]string{"query": req.Message})
		_ = stream.SendToolCallStart("tc_001", "search", args)
		time.Sleep(500 * time.Millisecond) // Simulate tool execution
		_ = stream.SendToolCallEnd("tc_001")

		// Simulate tool output
		output, _ := json.Marshal(map[string]string{"result": "Here's what I found: ..."})
		_ = stream.SendToolOutput("tc_001", output, false)

		_ = stream.SendTextChunk("\nBased on the search results, here's my answer.")
		_ = stream.SendTextEnd()
	})

	// Register a demo handler that uses HITL
	coord.RegisterHandler("approval-demo", func(req RunRequest, stream *RunStream) {
		_ = stream.SendTextStart("")
		_ = stream.SendTextChunk("I need your approval to proceed.\n\n")

		// Simulate HITL interrupt
		payload, _ := json.Marshal(map[string]interface{}{
			"action":  "delete_file",
			"target":  "/data/important.csv",
			"warning": "This action cannot be undone",
		})

		resp, err := stream.SendInterruptRequired("int_001", "approve_file_deletion", payload)
		if err != nil {
			_ = stream.SendRunError(fmt.Sprintf("interrupt failed: %v", err), "INTERRUPT_FAILED", false)
			return
		}

		if resp.Approved {
			_ = stream.SendTextChunk("✅ File deletion approved. Proceeding...\n")
			time.Sleep(500 * time.Millisecond)
			_ = stream.SendTextChunk("File deleted successfully.")
		} else {
			_ = stream.SendTextChunk("❌ File deletion denied. Operation cancelled.")
		}

		_ = stream.SendTextEnd()
	})

	return coord
}
