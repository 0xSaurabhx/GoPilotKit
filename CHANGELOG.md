# Changelog

All notable changes to GoPilotKit will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Config engine with YAML validation and environment variable expansion
- Three deployment profiles: personal, team, enterprise
- AG-UI SSE streaming protocol with 18 typed events
- AG-UI HTTP handler (`/run`, `/runs`, `/health`)
- AG-UI RunStream high-level API for writing events
- AG-UI Coordinator with HITL interrupt support
- RunStream context propagation and panic recovery
- OpenAI LLM provider with streaming support
- Anthropic LLM provider with streaming support
- Ollama LLM provider for local inference
- Circuit breaker with state machine (closed → open → half-open)
- Fallback provider with automatic retry on retryable errors
- Pure-Go SQLite thread store with WAL mode
- Thread CRUD operations (create, get, update, delete, list, rename, archive)
- Message persistence with thread association
- Run state persistence and tracking
- Cascade deletes for thread-related data
- CLI with `run`, `validate`, and `version` commands
- Makefile with build, test, and compression targets
- CI pipeline (GitHub Actions)
- Release pipeline with multi-platform binaries
- Sample configs for personal, team, and enterprise profiles
- Apache 2.0 license
- README with architecture diagrams and quickstart guide
- Contributing guidelines and code of conduct
- GitHub issue and PR templates

### Fixed
- ListThreads archived query args mismatch (args included hardcoded WHERE value)

## [0.1.0] - 2026-08-23

### Added
- Initial release scaffolding
