package wasm

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// buildTestWASM compiles the mock WASM server from testdata to a .wasm binary.
func buildTestWASM(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	wasmPath := filepath.Join(tmpDir, "test.wasm")

	// Build the mock WASM server.
	srcPath := "testdata/mock_wasm_server.go"
	cmd := exec.Command("go", "build", "-o", wasmPath, srcPath)
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot build test WASM module (GOOS=wasip1): %v\n%s", err, out)
	}

	return wasmPath
}

func TestModuleInitialize(t *testing.T) {
	wasmPath := buildTestWASM(t)

	mod, err := NewModule(wasmPath, 10*time.Second)
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	defer mod.Close()

	ctx := context.Background()
	if err := mod.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	if !mod.IsReady() {
		t.Fatal("expected module to be ready after Initialize")
	}
}

func TestModuleListTools(t *testing.T) {
	wasmPath := buildTestWASM(t)

	mod, err := NewModule(wasmPath, 10*time.Second)
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	defer mod.Close()

	ctx := context.Background()
	if err := mod.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	tools, err := mod.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name] = true
	}

	if !names["echo"] {
		t.Error("expected 'echo' tool")
	}
	if !names["add"] {
		t.Error("expected 'add' tool")
	}
}

func TestModuleCallEchoTool(t *testing.T) {
	wasmPath := buildTestWASM(t)

	mod, err := NewModule(wasmPath, 10*time.Second)
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	defer mod.Close()

	ctx := context.Background()
	if err := mod.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	args, _ := json.Marshal(map[string]string{"message": "hello WASM"})
	result, err := mod.CallTool(ctx, "echo", args)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if result.IsError {
		t.Fatal("expected no error in result")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}
	if result.Content[0].Text != "Echo: hello WASM" {
		t.Errorf("expected 'Echo: hello WASM', got %q", result.Content[0].Text)
	}
}

func TestModuleCallAddTool(t *testing.T) {
	wasmPath := buildTestWASM(t)

	mod, err := NewModule(wasmPath, 10*time.Second)
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	defer mod.Close()

	ctx := context.Background()
	if err := mod.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	args, _ := json.Marshal(map[string]float64{"a": 3, "b": 7})
	result, err := mod.CallTool(ctx, "add", args)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if result.IsError {
		t.Fatal("expected no error in result")
	}
	if result.Content[0].Text != "10" {
		t.Errorf("expected '10', got %q", result.Content[0].Text)
	}
}

func TestModuleCallUnknownTool(t *testing.T) {
	wasmPath := buildTestWASM(t)

	mod, err := NewModule(wasmPath, 10*time.Second)
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	defer mod.Close()

	ctx := context.Background()
	if err := mod.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	args, _ := json.Marshal(map[string]string{})
	_, err = mod.CallTool(ctx, "nonexistent", args)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestNewModuleInvalidPath(t *testing.T) {
	_, err := NewModule("/nonexistent/path.wasm", 5*time.Second)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}
