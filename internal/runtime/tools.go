package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/0xsaurabhx/GoPilotKit/internal/config"
)

// ToolExecutor handles tool invocation. Currently implements a stub executor
// that returns "tool not available" for all tools. Real implementations will
// connect to MCP servers and WASM plugins.
type ToolExecutor struct {
	tools map[string]*config.ToolConfig
}

// NewToolExecutor creates a tool executor from the configured tools.
func NewToolExecutor(toolConfigs []config.ToolConfig) *ToolExecutor {
	te := &ToolExecutor{
		tools: make(map[string]*config.ToolConfig),
	}
	for i := range toolConfigs {
		te.tools[toolConfigs[i].ID] = &toolConfigs[i]
	}
	return te
}

// Execute runs a tool by name with the given arguments.
// Returns the tool output as JSON bytes, or an error if execution fails.
//
// Tool execution flow:
//  1. Look up the tool config by name.
//  2. Route to the appropriate executor (MCP, WASM).
//  3. Return the result.
//
// Current implementation is a stub — all tools return "not available".
// This will be fleshed out when MCP and WASM support are wired in.
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
// TODO: Implement stdio/SSE transport to MCP servers.
func (te *ToolExecutor) executeMCP(ctx context.Context, cfg *config.ToolConfig, args string) ([]byte, error) {
	// Stub: return a placeholder indicating MCP is not yet wired.
	output := map[string]interface{}{
		"error": fmt.Sprintf("MCP tool %q is configured but MCP host is not yet wired. "+
			"Configure the MCP server and implement the stdio/SSE transport.", cfg.ID),
	}
	data, _ := json.Marshal(output)
	return data, nil
}

// executeWASM handles tool execution via WASM plugins.
// TODO: Implement wazero-based WASM execution.
func (te *ToolExecutor) executeWASM(ctx context.Context, cfg *config.ToolConfig, args string) ([]byte, error) {
	// Stub: return a placeholder indicating WASM is not yet wired.
	output := map[string]interface{}{
		"error": fmt.Sprintf("WASM tool %q is configured but the WASM runtime is not yet wired. "+
			"Implement the wazero-based plugin loader.", cfg.ID),
	}
	data, _ := json.Marshal(output)
	return data, nil
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
