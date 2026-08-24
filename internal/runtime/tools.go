package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/0xsaurabhx/GoPilotKit/internal/config"
	"github.com/0xsaurabhx/GoPilotKit/internal/mcp"
	"github.com/0xsaurabhx/GoPilotKit/internal/wasm"
)

// ToolExecutor handles tool invocation by routing to MCP servers or WASM plugins.
// MCP servers are connected lazily on first tool invocation.
// WASM modules are compiled at startup and instantiated per-invocation.
type ToolExecutor struct {
	tools    map[string]*config.ToolConfig
	mcpHost  *mcp.Host
	mcpMap   map[string]string // tool ID → MCP server ID
	wasmMods map[string]*wasm.Module // tool ID → compiled WASM module
}

// NewToolExecutor creates a tool executor from the configured tools.
// mcpHost may be nil if no MCP servers are configured.
func NewToolExecutor(toolConfigs []config.ToolConfig, mcpHost *mcp.Host) *ToolExecutor {
	te := &ToolExecutor{
		tools:    make(map[string]*config.ToolConfig),
		mcpHost:  mcpHost,
		mcpMap:   make(map[string]string),
		wasmMods: make(map[string]*wasm.Module),
	}
	for i := range toolConfigs {
		te.tools[toolConfigs[i].ID] = &toolConfigs[i]
	}
	return te
}

// SetMCPMap sets the mapping from tool ID to MCP server ID.
// Called by the engine after connecting MCP servers.
func (te *ToolExecutor) SetMCPMap(m map[string]string) {
	te.mcpMap = m
}

// LoadWASMModule compiles and initializes a WASM tool module.
// Called by the engine at startup for each WASM tool.
func (te *ToolExecutor) LoadWASMModule(ctx context.Context, toolID, binaryPath string, timeout time.Duration) error {
	mod, err := wasm.NewModule(binaryPath, timeout)
	if err != nil {
		return fmt.Errorf("compiling WASM module for tool %q: %w", toolID, err)
	}
	if err := mod.Initialize(ctx); err != nil {
		mod.Close()
		return fmt.Errorf("initializing WASM module for tool %q: %w", toolID, err)
	}
	te.wasmMods[toolID] = mod
	log.Printf("runtime: WASM module loaded for tool %q from %s", toolID, binaryPath)
	return nil
}

// Execute runs a tool by name with the given arguments.
// Returns the tool output as JSON bytes, or an error if execution fails.
func (te *ToolExecutor) Execute(ctx context.Context, toolName string, args string) ([]byte, error) {
	toolCfg, ok := te.tools[toolName]
	if !ok {
		return nil, fmt.Errorf("tool %q not found in configuration", toolName)
	}

	log.Printf("runtime: executing tool %q (type=%s)", toolName, toolCfg.Type)

	switch toolCfg.Type {
	case "mcp":
		return te.executeMCP(ctx, toolCfg, args)
	case "wasm":
		return te.executeWASM(ctx, toolCfg, args)
	default:
		return nil, fmt.Errorf("unsupported tool type: %s", toolCfg.Type)
	}
}

// executeMCP handles tool execution via MCP (Model Context Protocol).
// It lazily connects to the MCP server on first invocation, then calls the tool.
func (te *ToolExecutor) executeMCP(ctx context.Context, cfg *config.ToolConfig, args string) ([]byte, error) {
	if te.mcpHost == nil {
		return nil, fmt.Errorf("MCP host not initialized — no MCP servers configured")
	}

	serverID := te.mcpMap[cfg.ID]
	if serverID == "" {
		return nil, fmt.Errorf("MCP tool %q has no associated server", cfg.ID)
	}

	// Lazy-connect: if the server isn't connected yet, connect now.
	if !te.mcpHost.HasServer(serverID) {
		log.Printf("runtime: lazily connecting MCP server %q for tool %q", serverID, cfg.ID)
		if err := te.mcpHost.ConnectServer(ctx, mcp.ServerConfig{
			ID:      serverID,
			Command: cfg.Command,
			Args:    cfg.Args,
			Timeout: 30 * time.Second,
		}); err != nil {
			return nil, fmt.Errorf("connecting MCP server %q: %w", serverID, err)
		}
	}

	client, ok := te.mcpHost.GetClient(serverID)
	if !ok {
		return nil, fmt.Errorf("MCP server %q not found after connection", serverID)
	}

	// Discover tools on the server and verify our tool exists.
	serverTools, err := client.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing MCP tools on server %q: %w", serverID, err)
	}

	var found bool
	for _, st := range serverTools {
		if st.Name == cfg.ID {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("tool %q not found on MCP server %q (available: %v)", cfg.ID, serverID, toolNames(serverTools))
	}

	// Call the tool.
	result, err := client.CallTool(ctx, cfg.ID, json.RawMessage(args))
	if err != nil {
		return nil, fmt.Errorf("MCP tool %q call failed: %w", cfg.ID, err)
	}

	// Convert MCP ToolResult to JSON bytes.
	output := map[string]interface{}{
		"content":  result.Content,
		"is_error": result.IsError,
	}
	return json.Marshal(output)
}

// executeWASM handles tool execution via a WASM plugin.
// It uses the pre-compiled module to spawn a fresh WASM instance per call.
func (te *ToolExecutor) executeWASM(ctx context.Context, cfg *config.ToolConfig, args string) ([]byte, error) {
	mod, ok := te.wasmMods[cfg.ID]
	if !ok {
		// No pre-loaded module — try to compile on the fly (for lazy loading).
		if cfg.BinaryPath == "" {
			return nil, fmt.Errorf("WASM tool %q has no binary_path configured", cfg.ID)
		}
		log.Printf("runtime: lazily loading WASM module for tool %q from %s", cfg.ID, cfg.BinaryPath)
		var err error
		mod, err = wasm.NewModule(cfg.BinaryPath, 30*time.Second)
		if err != nil {
			return nil, fmt.Errorf("compiling WASM tool %q: %w", cfg.ID, err)
		}
		if err := mod.Initialize(ctx); err != nil {
			mod.Close()
			return nil, fmt.Errorf("initializing WASM tool %q: %w", cfg.ID, err)
		}
		te.wasmMods[cfg.ID] = mod
	}

	// Call the tool on the WASM module.
	result, err := mod.CallTool(ctx, cfg.ID, json.RawMessage(args))
	if err != nil {
		return nil, fmt.Errorf("WASM tool %q call failed: %w", cfg.ID, err)
	}

	output := map[string]interface{}{
		"content":  result.Content,
		"is_error": result.IsError,
	}
	return json.Marshal(output)
}

// ListTools returns the names of all configured tools.
func (te *ToolExecutor) ListTools() []string {
	names := make([]string, 0, len(te.tools))
	for name := range te.tools {
		names = append(names, name)
	}
	return names
}

// HasTool returns true if a tool with the given name is configured.
func (te *ToolExecutor) HasTool(name string) bool {
	_, ok := te.tools[name]
	return ok
}

// Close releases WASM modules held by the executor.
func (te *ToolExecutor) Close() {
	for id, mod := range te.wasmMods {
		if err := mod.Close(); err != nil {
			log.Printf("runtime: error closing WASM module %q: %v", id, err)
		}
	}
}

// toolNames extracts tool names from a list of MCP tools.
func toolNames(tools []mcp.Tool) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}
