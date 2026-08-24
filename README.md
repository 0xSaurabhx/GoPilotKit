# GoPilotKit

Enterprise intelligence runtime in a single Go binary. Implements AG-UI, MCP, and A2A protocols with autonomous self-improvement, persistent threads, guardrails, analytics, and multi-surface channel adapters.

~11MB. Zero CGO. Deploys anywhere.

## Getting Started

**Install**

```bash
go install github.com/0xsaurabhx/GoPilotKit/cmd/engine@latest
```

**Build from source**

```bash
git clone https://github.com/0xsaurabhx/GoPilotKit.git
cd GoPilotKit
make build
```

**Run**

```bash
cp configs/personal.yaml intelligence.yaml
# Edit intelligence.yaml with your API keys

./bin/gopilotkit validate --config intelligence.yaml
./bin/gopilotkit run --config intelligence.yaml
```

**Send a test message**

```bash
curl -N -X POST http://localhost:8080/run \
  -H "Content-Type: application/json" \
  -d '{"agent": "personal-assistant", "message": "Hello, what can you do?"}'
```

## Protocols

| Protocol | Layer | Notes |
|----------|-------|-------|
| AG-UI | Agent ↔ User | SSE streaming, WebSocket, gRPC, HITL interrupts, generative UI, state management, thinking events, sub-agent delegation |
| MCP | Agent ↔ Tools | JSON-RPC 2.0, stdio and SSE transport, tool discovery & execution, lazy server connection |
| A2A | Agent ↔ Agent | Agent Cards, skill definitions, task lifecycle, delegation manager |

### AG-UI Transports

| Transport | Endpoint | Notes |
|-----------|----------|-------|
| SSE | `/run` | Full event streaming with heartbeat keepalive |
| WebSocket | `/ws` | Bidirectional, ping/pong keepalive |
| gRPC | port+1 | Streaming Run RPC, client helpers with metadata headers |

### AG-UI Events

`RUN_START`, `TEXT_START`, `TEXT_CHUNK`, `TEXT_END`, `TOOL_CALL_START`, `TOOL_CALL_CHUNK`, `TOOL_CALL_END`, `TOOL_OUTPUT`, `INTERRUPT_REQUIRED`, `STATE_DELTA`, `STATE_SNAPSHOT`, `THINKING_START`, `THINKING_CHUNK`, `THINKING_END`, `SUB_AGENT_START`, `SUB_AGENT_END`, `GEN_UI_PAYLOAD`, `RUN_END`, `RUN_ERROR`, `CUSTOM`

## LLM Providers

| Provider | Models | Streaming | Tool Calls |
|----------|--------|-----------|------------|
| OpenAI | GPT-4o, GPT-4o-mini, GPT-5 | Yes | Yes |
| Anthropic | Claude 3.5 Sonnet, Claude 3.5 Haiku | Yes | Yes |
| Ollama | Local inference | Yes | Yes |
| Google | Gemini Pro, Gemini Flash | Yes | Yes |
| Mistral | Mistral Large, Mistral Small | Yes | Yes |
| Cohere | Command R+, Command R | Yes | Yes |

All providers include circuit breaker (auto-open after 5 failures, 30s reset, half-open recovery) and primary/secondary provider fallback with retry on retryable errors (429, 500, 503).

## Storage

| Driver | Backend | Notes |
|--------|---------|-------|
| SQLite | Pure-Go (modernc) | WAL mode, `busy_timeout`, zero CGO |
| PostgreSQL | pgx/v5 | Connection pooling, automatic schema migration |

### Thread Store

- Thread CRUD: create, get, update, delete, list, rename, archive
- Message persistence with thread association and sliding window
- Run state persistence and tracking (running, completed, error)
- Cascade deletes for thread-related data

## Self-Improvement Engine

The agent learns from real usage without human intervention.

| Capability | Description |
|------------|-------------|
| Signal Extraction | 8 signal types: positive, negative, correction, implicit, tool retry, clarification |
| Evaluation | Rule-based scoring (helpfulness, accuracy, safety), regression testing, baselines |
| Prompt Evolution | Versioned prompts, automatic rewriting from failure patterns, rollback |
| Few-Shot Curation | Similarity retrieval, quality scoring, auto-eviction (max 100 examples) |
| Skill Recording | Learns tasks from demonstrations, success rate tracking |
| Memory Optimization | Strength-based storage, time decay, consolidation, eviction |
| Container Scoping | Learning isolated per user/org/project/agent |

All improvements are auditable and traceable to their source interaction.

## RAG (Retrieval-Augmented Generation)

### Vector Store Backends

| Backend | Dependencies | Notes |
|---------|-------------|-------|
| Embedded | None | In-memory TF-IDF, cosine similarity |
| Qdrant | Qdrant server | HTTP API, collection management |
| pgvector | PostgreSQL + pgvector | HNSW index, cosine distance operator |

### Capabilities

- Document ingestion with automatic chunking (configurable size and overlap)
- TF-IDF vector storage with L2-normalized embeddings
- Cosine similarity search with configurable minimum score
- Agentic retrieval: the agent decides when retrieval is needed based on query type
- Document count, chunk count, and per-document deletion

## Guardrails

| Guardrail | Description |
|-----------|-------------|
| PII Scrubbing | Email, phone, SSN, credit card, IP detection and redaction |
| Content Filtering | Jailbreak detection, prompt injection blocking |
| Blocked Words | Configurable word/phrase blocking |
| Input Validation | Pre-LLM content checks (CheckInput) |
| Output Validation | Post-LLM content checks (CheckOutput) |

Three severity levels: low (logged), medium (scrubbed/masked), high (blocked entirely).

## Analytics

In-memory event store with configurable retention. Real-time dashboard (total runs, active runs, token usage, error rate, hourly buckets). Event queries filterable by agent, thread, run, type, and time range. OTLP export for DataDog, NewRelic, and Grafana. Per-request, per-user, per-org cost tracking.

### HTTP Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/dashboard` | GET | Real-time analytics dashboard |
| `/events` | GET | Query events with filters (agent, thread_id, run_id, event_type, since, limit) |
| `/analytics` | GET | Summary with total tokens, cost, and event count |

## Cost Management

Token counting (prompt, completion, cached). Per-provider pricing tables (OpenAI, Anthropic) with custom overrides. Daily and monthly budget enforcement. Rate limiting by requests/minute and tokens/minute. Aggregated usage summaries by provider, agent, and time period.

### Default Pricing

| Provider | Model | Prompt/Token | Completion/Token |
|----------|-------|-------------|-----------------|
| OpenAI | gpt-4o | $0.0000025 | $0.00001 |
| OpenAI | gpt-4o-mini | $0.00000015 | $0.0000006 |
| Anthropic | claude-3-5-sonnet | $0.000003 | $0.000015 |
| Anthropic | claude-3-5-haiku | $0.0000008 | $0.000004 |

## Semantic Caching

TF-IDF vector-based similarity search with no external embedding dependency. Cosine similarity matching with configurable threshold (default 0.85). TTL-based expiration, hit counting, automatic eviction at capacity (default 10,000 entries). L2-normalized feature vectors with padded dimension matching.

## Channels

| Channel | Streaming | Push Notifications | Notes |
|---------|-----------|-------------------|-------|
| Web | Yes (SSE) | — | Full SSE streaming, rich JSON messages |
| WebSocket | Yes | — | Bidirectional, ping/pong keepalive |
| Mobile | Yes | FCM + APNs | Device registration, batched chunks |
| Slack | No | — | Event API format, block-based formatting |
| Teams | No | — | Bot Framework message format |

## Enterprise Features

| Feature | Description |
|---------|-------------|
| SSO | OIDC and GitHub OAuth, session management, email domain allowlisting |
| RBAC | Role-based access control with admin/operator/viewer defaults, 14 permission types |
| Audit Logging | JSON structured logs, severity filtering, file/stdout/stderr output, query by type and time range |
| Data Retention | Configurable event retention with automatic purge |

### RBAC Roles

| Role | Permissions |
|------|------------|
| admin | Full system access (14 permissions) |
| operator | Run agents, manage threads, view analytics (6 permissions) |
| viewer | Read-only access (3 permissions) |

## CLI

```bash
gopilotkit run --config intelligence.yaml      # Start the runtime server
gopilotkit validate --config intelligence.yaml  # Validate configuration
gopilotkit version                              # Print version
```

## API

| Method | Path | Description |
|--------|------|-------------|
| POST | /run | Start an agent run (SSE stream) |
| GET | /runs | List active runs |
| GET | /health | Health check |
| GET | /dashboard | Analytics dashboard |
| GET | /events | Query events |
| GET | /analytics | Usage summary |

### SSE Events

`RUN_START`, `TEXT_START`, `TEXT_CHUNK`, `TEXT_END`, `TOOL_CALL_START`, `TOOL_CALL_END`, `TOOL_OUTPUT`, `INTERRUPT_REQUIRED`, `STATE_DELTA`, `STATE_SNAPSHOT`, `RUN_END`, `RUN_ERROR`

## Configuration

Single YAML file with three profiles:

```yaml
version: "v1alpha"
profile: "personal"  # personal | team | enterprise

server:
  port: 8080
  transport: ["sse"]  # sse | ws | grpc

storage:
  driver: "sqlite"    # sqlite | postgres
  connection_uri: "./data/gopilotkit.db"

providers:
  - id: "primary-llm"
    type: "openai"    # openai | anthropic | ollama | google | mistral | cohere
    model: "gpt-4o"
    api_key: "${OPENAI_API_KEY}"

agents:
  - name: "personal-assistant"
    provider: "primary-llm"
    system_prompt: |
      You are a helpful personal AI assistant.
      Be concise and accurate.
    memory:
      sliding_window: 20
      semantic_recall: true

tools:
  - id: "calculator"
    type: "mcp"
    command: "node"
    args: ["calculator-mcp/index.js"]

analytics:
  enabled: true
  dashboard: true
```

See [`configs/`](configs/) for personal, team, and enterprise examples.

Environment variables use `${VAR}` or `${VAR|default}` syntax:

```yaml
api_key: "${OPENAI_API_KEY}"
connection_uri: "${DATABASE_URL|postgres://localhost:5432/gopilotkit}"
```

## Docker

```bash
docker build -t gopilotkit .
docker run -p 8080:8080 -v ./configs:/data gopilotkit
```

## Build

| Target | Command | Output |
|--------|---------|--------|
| Production | `make build` | Stripped binary (~11MB) |
| Debug | `make build-dev` | With debug symbols |
| Compressed | `make build-small` | UPX compressed (~5MB) |
| Tests | `make test` | All packages |
| Coverage | `make test-cover` | With coverage report |
| Lint | `make lint` | Static analysis |
| Docker | `docker build` | Multi-stage scratch image |

Cross-compiles with `CGO_ENABLED=0`.

## Architecture

```
Client Layer (Web / Mobile / Slack / Teams)
       |
  Channel Adapters
       v
AG-UI Protocol Layer (SSE / WebSocket / gRPC)
       |
  Runtime Engine
  - Config Engine, Provider Router (CB/FB), Guardrails, Cost Tracker
  - Prompt Builder → LLM Stream → Tool Loop (max 10 rounds)
  - Thread Store (SQLite WAL / PostgreSQL) + Analytics Store
       |
  Autonomous Learning Pipeline
  Signals → Evaluation → Prompts / Few-Shot / Skills / Memory
       |
  MCP Host (stdio / SSE), WASM Host (wazero), Semantic Cache, A2A Protocol
```

## Project Structure

```
cmd/engine/                  CLI entrypoint (Cobra)
internal/
  config/                    YAML config, validation, env vars
  protocol/
    agui/                    AG-UI SSE/WebSocket/gRPC protocol
    a2a/                     A2A agent-to-agent protocol
  runtime/                   Runtime engine (orchestration loop, tools, learning)
  provider/                  LLM connectors (OpenAI, Anthropic, Ollama, Google, Mistral, Cohere)
  store/
    threads/                 SQLite/PostgreSQL thread/message/run store
    analytics/               Analytics event store + dashboard + OTLP export
  learning/                  Autonomous self-improvement
    signals/                 Signal extraction from interactions
    evaluation/              Quality scoring and regression testing
    prompts/                 Prompt versioning and evolution
    fewshot/                 Few-shot example curation
    memory/                  Memory optimization with decay
    skills/                  Skill recording from demonstrations
    containers/              Container-scoped learning isolation
  mcp/                       MCP host (JSON-RPC 2.0, stdio + SSE transport)
  wasm/                      WASM plugin runtime (wazero, WASI)
  rag/                       Agentic RAG (embedded/qdrant/pgvector)
  guardrails/                PII scrubbing, content filtering
  cost/                      Token counting, budgets, rate limiting
  cache/                     Semantic caching (TF-IDF)
  channels/                  Multi-surface adapters (mobile with FCM/APNs)
  enterprise/                SSO, RBAC, audit logging
configs/                     Sample configuration files (personal, team, enterprise)
plugins/                     Prebuilt WASM plugins
```

## Roadmap

### Completed

- [x] AG-UI streaming protocol (SSE, WebSocket, gRPC)
- [x] MCP host with stdio and SSE transport
- [x] A2A agent-to-agent protocol with delegation
- [x] Autonomous self-improvement engine (7 subsystems)
- [x] SQLite and PostgreSQL thread storage
- [x] Guardrails: PII, content filter, blocked words
- [x] Analytics dashboard with OTLP export
- [x] Cost management with budgets and rate limiting
- [x] Semantic caching with TF-IDF
- [x] Multi-channel adapters (Web, Mobile with push)
- [x] 6 LLM providers with circuit breaker and fallback
- [x] Agentic RAG with 3 vector store backends
- [x] WASM plugin system (wazero)
- [x] CLI with run, validate, version commands
- [x] REST API with SSE event streaming
- [x] Enterprise SSO, RBAC, audit logging
- [x] Docker multi-stage build (scratch image)
- [x] CI/CD pipeline (GitHub Actions)

### Upcoming

- [ ] MCP gRPC transport
- [ ] Embedding-based vector cache (OpenAI embeddings)
- [ ] Batch processing mode
- [ ] Multi-agent orchestration workflows
- [ ] Prometheus/Grafana metrics endpoint
- [ ] Plugin marketplace
- [ ] Slack and Teams channel adapters (full implementations)
- [ ] Structured output with JSON schema validation

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Apache License 2.0. See [LICENSE](LICENSE).

## Acknowledgments

Built on [AG-UI Protocol](https://docs.ag-ui.com/), [MCP](https://modelcontextprotocol.io/), [A2A](https://github.com/a2aproject/A2A), and [CopilotKit](https://www.copilotkit.ai/).
