# Changelog

All notable changes to GoPilotKit will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.8.0] - 2026-08-24

### Added

**Protocols**
- AG-UI SSE streaming protocol with 18 typed events
- AG-UI WebSocket transport with ping/pong keepalive
- AG-UI gRPC transport with streaming Run RPC and client helpers
- MCP host with stdio and SSE transport (JSON-RPC 2.0)
- A2A agent-to-agent protocol with Agent Cards, tasks, and delegation manager

**LLM Providers**
- OpenAI provider with streaming and tool calls
- Anthropic provider with streaming and tool calls
- Ollama provider for local inference
- Google Gemini provider with streaming and tool calls
- Mistral provider with streaming and tool calls
- Cohere provider with streaming and tool calls
- Circuit breaker with state machine (closed → open → half-open)
- Fallback provider with automatic retry on retryable errors

**Storage**
- Pure-Go SQLite thread store with WAL mode and busy_timeout
- PostgreSQL thread store with pgx/v5 connection pooling
- Thread CRUD (create, get, update, delete, list, rename, archive)
- Message persistence with sliding window
- Run state persistence and tracking
- Cascade deletes for thread-related data

**RAG**
- Embedded TF-IDF vector store (no external dependencies)
- Qdrant vector store backend
- pgvector vector store backend with HNSW index
- Document ingestion with automatic chunking
- Agentic retrieval with configurable similarity threshold

**Self-Improvement Engine**
- Signal extraction (8 types: positive, negative, correction, implicit, tool retry, clarification)
- Evaluation engine with rule-based scoring (helpfulness, accuracy, safety)
- Prompt evolution with versioning and rollback
- Few-shot example curation with similarity retrieval
- Memory optimization with time decay and consolidation
- Skill recording from demonstrations
- Container-scoped learning isolation

**Guardrails**
- PII scrubbing (email, phone, SSN, credit card, IP)
- Content filtering (jailbreak, prompt injection detection)
- Blocked words configuration
- Input validation (pre-LLM)
- Output validation (post-LLM)
- Three severity levels: low, medium, high

**Analytics**
- In-memory event store with configurable retention
- Real-time dashboard (runs, tokens, cost, error rate, hourly buckets)
- Event queries with agent, thread, run, type, and time filters
- OTLP export for DataDog, NewRelic, and Grafana
- Per-request, per-user, per-org cost tracking

**Cost Management**
- Token counting (prompt, completion, cached)
- Per-provider pricing tables with custom overrides
- Daily and monthly budget enforcement
- Rate limiting by requests/minute and tokens/minute
- Aggregated usage summaries

**Semantic Caching**
- TF-IDF vector-based similarity search
- Cosine similarity with configurable threshold
- TTL-based expiration and hit counting
- Automatic eviction at capacity

**Channels**
- Web channel with SSE streaming
- Mobile channel with FCM (Android) and APNs (iOS) push notifications
- Device token registration and management
- Batched streaming chunks for mobile

**Enterprise**
- SSO with OIDC and GitHub OAuth providers
- Session management with token validation and expiry
- Email domain allowlisting
- RBAC with admin/operator/viewer roles and 14 permission types
- Audit logging with JSON structured output, severity filtering
- HTTP middleware for SSO and RBAC enforcement
- Data retention with configurable purge

**Infrastructure**
- CLI with run, validate, and version commands
- REST API with /run, /runs, /health, /dashboard, /events, /analytics endpoints
- Docker multi-stage build (scratch image, ~11MB)
- CI pipeline (GitHub Actions) with Go 1.23/1.24 matrix
- Release pipeline with multi-platform binaries (linux/darwin/windows, amd64/arm64)
- Config engine with YAML validation and ${VAR} environment variable expansion
- Three deployment profiles: personal, team, enterprise

### Fixed
- ListThreads archived query args mismatch (args included hardcoded WHERE value)

## [0.1.0] - 2026-08-23

### Added
- Initial release scaffolding
