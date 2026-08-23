// Package wasm implements a sandboxed plugin runtime for WASM plugins.
//
// Uses an isolated execution environment to run community plugins written in
// Rust, Go, or TypeScript without granting direct host OS access.
// This package provides the plugin interface; the actual WASM runtime (wazero)
// is wired in when the dependency is available.
package wasm

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Plugin represents a loaded WASM plugin.
type Plugin struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	BinaryPath  string          `json:"binary_path"`
	Functions   []FunctionDef  `json:"functions"`
	LoadedAt    time.Time       `json:"loaded_at"`
	Active      bool            `json:"active"`
}

// FunctionDef defines a function exposed by a plugin.
type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema
	Returns     json.RawMessage `json:"returns"`    // JSON Schema
}

// PluginRequest is a request to invoke a plugin function.
type PluginRequest struct {
	Function string          `json:"function"`
	Args     json.RawMessage `json:"args"`
}

// PluginResponse is the result of invoking a plugin function.
type PluginResponse struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	TimeMs int64           `json:"time_ms"`
}

// PluginManager manages WASM plugins.
type PluginManager struct {
	plugins map[string]*Plugin
	mu      sync.RWMutex
}

// NewPluginManager creates a new plugin manager.
func NewPluginManager() *PluginManager {
	return &PluginManager{
		plugins: make(map[string]*Plugin),
	}
}

// LoadPlugin loads a WASM plugin from a binary file.
// In production, this would use wazero to compile and instantiate the WASM module.
func (pm *PluginManager) LoadPlugin(id, name, version, description, binaryPath string, functions []FunctionDef) *Plugin {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	plugin := &Plugin{
		ID:          id,
		Name:        name,
		Version:     version,
		Description: description,
		BinaryPath:  binaryPath,
		Functions:   functions,
		LoadedAt:    time.Now().UTC(),
		Active:      true,
	}
	pm.plugins[id] = plugin
	return plugin
}

// Invoke calls a function on a loaded plugin.
// Currently returns a stub response; real implementation would execute via wazero.
func (pm *PluginManager) Invoke(pluginID string, req PluginRequest) (*PluginResponse, error) {
	pm.mu.RLock()
	plugin, ok := pm.plugins[pluginID]
	pm.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("plugin %q not found", pluginID)
	}
	if !plugin.Active {
		return nil, fmt.Errorf("plugin %q is not active", pluginID)
	}

	// Verify the function exists.
	found := false
	for _, fn := range plugin.Functions {
		if fn.Name == req.Function {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("function %q not found in plugin %q", req.Function, pluginID)
	}

	// Stub: return a placeholder response.
	// Real implementation would call the WASM function via wazero.
	return &PluginResponse{
		Result: json.RawMessage(`{"status":"stub","message":"WASM runtime not wired — install wazero to execute plugins"}`),
		TimeMs: 0,
	}, nil
}

// GetPlugin returns a plugin by ID.
func (pm *PluginManager) GetPlugin(id string) (*Plugin, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	p, ok := pm.plugins[id]
	return p, ok
}

// ListPlugins returns all loaded plugins.
func (pm *PluginManager) ListPlugins() []*Plugin {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	var plugins []*Plugin
	for _, p := range pm.plugins {
		plugins = append(plugins, p)
	}
	return plugins
}

// UnloadPlugin removes a plugin.
func (pm *PluginManager) UnloadPlugin(id string) bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if _, ok := pm.plugins[id]; ok {
		delete(pm.plugins, id)
		return true
	}
	return false
}

// Count returns the number of loaded plugins.
func (pm *PluginManager) Count() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.plugins)
}
