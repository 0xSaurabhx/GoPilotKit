# Contributing to GoPilotKit

Thank you for your interest in contributing to GoPilotKit! This document provides guidelines and instructions for contributing.

## Getting Started

1. **Fork** the repository
2. **Clone** your fork: `git clone https://github.com/your-username/GoPilotKit.git`
3. **Create** a branch: `git checkout -b feature/my-feature`
4. **Make** your changes
5. **Test** your changes: `make test`
6. **Commit** your changes
7. **Push** to your fork: `git push origin feature/my-feature`
8. **Open** a Pull Request

## Development Setup

### Prerequisites

- Go 1.22+ (we target the latest stable release)
- Git
- Make (optional, but recommended)

### Building

```bash
# Build the binary
make build

# Build with debug symbols
make build-dev

# Run all tests
make test

# Run tests with coverage
make test-cover

# Run linter
make lint
```

### Project Structure

```
cmd/engine/              # CLI entrypoint
internal/
  config/                # YAML config, validation
  protocol/agui/         # AG-UI SSE streaming
  protocol/a2a/          # A2A agent-to-agent
  runtime/               # Graph execution engine
  learning/              # Self-improvement engine
  store/threads/         # SQLite/Postgres persistence
  provider/              # LLM connectors
  ...
```

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Use meaningful variable and function names
- Add comments for exported functions and complex logic
- Keep functions focused and short
- Handle errors explicitly — never ignore them

## Testing

- Write tests for all new functionality
- Use table-driven tests where appropriate
- Use `t.Helper()` in test helper functions
- Use `t.TempDir()` for temporary files/directories
- Aim for meaningful coverage, not just high percentages

```go
func TestSomething(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {"valid input", "hello", "HELLO", false},
        {"empty input", "", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := Process(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("Process() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if got != tt.want {
                t.Errorf("Process() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

## Commit Messages

Write clear, descriptive commit messages:

- `Add OpenAI provider with streaming support`
- `Fix circuit breaker not recovering after timeout`
- `Refactor config loader to support env var defaults`
- `Update AG-UI events to match latest spec`

Use the imperative mood ("Add feature" not "Added feature").

## Pull Request Guidelines

- Keep PRs focused on a single change
- Include tests for new functionality
- Update documentation if needed
- Ensure `make test` and `make vet` pass
- Write a clear PR description explaining what and why

## Reporting Bugs

When filing a bug report, please include:

1. GoPilotKit version (`gopilotkit version`)
2. Go version (`go version`)
3. OS and architecture
4. Steps to reproduce
5. Expected behavior
6. Actual behavior
7. Relevant logs or error messages

## Feature Requests

We welcome feature requests! Please open an issue with:

1. A clear description of the feature
2. The use case it solves
3. Any design ideas you have

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.
