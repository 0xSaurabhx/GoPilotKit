// Package wasm implements the WASM plugin runtime for GoPilotKit.
//
// WASM tools are standalone .wasm modules compiled with WASI support that
// communicate via stdin/stdout using the MCP-compatible JSON-RPC protocol.
// Each tool invocation spawns a fresh WASM instance, sends the request, and
// reads the response before tearing down the instance.
//
// Communication pattern:
//  1. Module starts and blocks reading from stdin
//  2. Host sends initialize request + tools/call request via stdin
//  3. Host closes stdin to signal end of input
//  4. Module processes requests and writes responses to stdout
//  5. Host reads responses from stdout
package wasm

import (
	"bytes"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// --- JSON-RPC types (MCP-compatible) ---

type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Tool is an MCP-compatible tool descriptor returned by tools/list.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ToolResult is the result of calling a tool.
type ToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent is a content block in a tool result.
type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	URI  string `json:"uri,omitempty"`
}

// --- Module ---

// Module represents a compiled WASM tool module. It caches the raw bytes
// and recompiles per runtime since wazero CompiledModules are bound to a single runtime.
type Module struct {
	path     string
	name     string
	wasmData []byte
	timeout  time.Duration
	mu       sync.Mutex
	ready    bool
}

// NewModule loads and compiles a .wasm file. The resulting Module is ready
// to be initialized and used for tool calls.
func NewModule(path string, timeout time.Duration) (*Module, error) {
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	wasmBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("wasm: reading %s: %w", path, err)
	}

	ctx := context.Background()
	r := wazero.NewRuntime(ctx)

	// Instantiate WASI for stdio support.
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, r); err != nil {
		r.Close(ctx)
		return nil, fmt.Errorf("wasm: instantiating WASI: %w", err)
	}

	r.Close(ctx)

	name := path
	if len(path) > 5 && path[len(path)-5:] == ".wasm" {
		name = path[:len(path)-5]
	}

	return &Module{
		path:     path,
		name:     name,
		wasmData: wasmBytes,
		timeout:  timeout,
	}, nil
}

// Name returns the module name (derived from the file path).
func (m *Module) Name() string { return m.name }

// IsReady returns true after Initialize has succeeded.
func (m *Module) IsReady() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ready
}

// Initialize runs a test invocation to verify the module responds to the
// MCP initialize handshake correctly.
func (m *Module) Initialize(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ready {
		return nil
	}

	// Verify the module works by running an initialize + tools/list round-trip.
	if err := m.roundTrip(ctx); err != nil {
		return fmt.Errorf("wasm: initialize failed for %s: %w", m.name, err)
	}

	m.ready = true
	log.Printf("wasm: module %s ready", m.name)
	return nil
}

// ListTools returns the tools exposed by the WASM module.
func (m *Module) ListTools(ctx context.Context) ([]Tool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.ready {
		return nil, fmt.Errorf("wasm: module %s not initialized", m.name)
	}

	responses, err := m.invokeLocked(ctx, jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      float64(2),
		Method:  "tools/list",
	})
	if err != nil {
		return nil, fmt.Errorf("wasm: listing tools on %s: %w", m.name, err)
	}

	raw, ok := responses[float64(2)]
	if !ok {
		return nil, fmt.Errorf("wasm: no tools/list response from %s", m.name)
	}

	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("wasm: parsing tools list from %s: %w", m.name, err)
	}
	return result.Tools, nil
}

// CallTool invokes a tool on the WASM module.
func (m *Module) CallTool(ctx context.Context, name string, args json.RawMessage) (*ToolResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.ready {
		return nil, fmt.Errorf("wasm: module %s not initialized", m.name)
	}

	responses, err := m.invokeLocked(ctx, jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      float64(2),
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      name,
			"arguments": args,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("wasm: calling tool %q on %s: %w", name, m.name, err)
	}

	raw, ok := responses[float64(2)]
	if !ok {
		return nil, fmt.Errorf("wasm: no tools/call response from %s", m.name)
	}

	var result ToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("wasm: parsing tool result from %s: %w", m.name, err)
	}
	return &result, nil
}

// Close releases the module resources.
func (m *Module) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wasmData = nil
	return nil
}

// roundTrip runs a full initialize + tools/list to verify the module works.
// Must be called with m.mu held.
func (m *Module) roundTrip(ctx context.Context) error {
	_, err := m.invokeLocked(ctx, jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "tools/list",
	})
	return err
}

// invokeLocked sends a JSON-RPC request to a fresh WASM instance and reads all responses.
// The WASM instance is instantiated with all requests pre-loaded into stdin,
// then stdin is closed so the module can process and respond.
// Must be called with m.mu held.
func (m *Module) invokeLocked(ctx context.Context, methodReq jsonRPCRequest) (map[interface{}]json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	// Build the input: initialize + notification + method request.
	var stdin bytes.Buffer
	writer := bufio.NewWriter(&stdin)

	// Initialize request
	initReq := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]string{
				"name":    "gopilotkit-wasm",
				"version": "1.0.0",
			},
		},
	}
	if err := writeJSONRPC(writer, initReq); err != nil {
		return nil, err
	}

	// Initialized notification
	if err := writeJSONRPC(writer, map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}); err != nil {
		return nil, err
	}

	// Method request
	if err := writeJSONRPC(writer, methodReq); err != nil {
		return nil, err
	}

	writer.Flush()

	// Create a fresh wazero runtime and compile the module.
	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	if _, err := wasi_snapshot_preview1.Instantiate(ctx, r); err != nil {
		return nil, fmt.Errorf("instantiating WASI: %w", err)
	}

	// Compile the module for this runtime.
	comp, err := r.CompileModule(ctx, m.wasmData)
	if err != nil {
		return nil, fmt.Errorf("compiling WASM: %w", err)
	}

	// Use in-memory buffers for stdin/stdout.
	stdout := &bytes.Buffer{}

	cfg := wazero.NewModuleConfig().
		WithName("").
		WithStdin(bytes.NewReader(stdin.Bytes())).
		WithStdout(stdout).
		WithStderr(os.Stderr)

	// Run the WASM module — it will read all of stdin and write responses to stdout.
	_, err = r.InstantiateModule(ctx, comp, cfg)
	if err != nil {
		return nil, fmt.Errorf("instantiating WASM module: %w", err)
	}

	// Parse all JSON-RPC responses from stdout.
	responses := make(map[interface{}]json.RawMessage)
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		var resp jsonRPCResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue // Skip non-JSON lines.
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		if resp.ID != nil {
			responses[resp.ID] = resp.Result
		}
	}

	return responses, nil
}

// writeJSONRPC writes a JSON-RPC message followed by a newline to a writer.
func writeJSONRPC(w io.Writer, msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}
