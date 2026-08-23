// Package wasm implements the sandboxed WASM plugin runtime.
//
// Uses wazero to execute community plugins (written in Rust, Go, or
// TypeScript) in-process without granting direct host OS access.
package wasm
