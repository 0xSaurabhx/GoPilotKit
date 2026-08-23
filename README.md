# GoPilotKit

<p align="center">
  <strong>Enterprise Intelligence Runtime — Single Binary, All Protocols</strong>
</p>

<p align="center">
  <a href="#quickstart">Quick Start</a> •
  <a href="#features">Features</a> •
  <a href="#architecture">Architecture</a> •
  <a href="#configuration">Configuration</a> •
  <a href="#development">Development</a> •
  <a href="#contributing">Contributing</a> •
  <a href="#license">License</a>
</p>

---

GoPilotKit is a **single Go binary** (~11MB) enterprise intelligence platform implementing all three agentic protocols — **AG-UI**, **MCP**, and **A2A** — with autonomous scaffold-based self-improvement, persistent threads, guardrails, analytics, and multi-surface channel adapters.

Deploy anywhere: from a laptop to Kubernetes to an air-gapped server.

## Quick Start

```bash
# Install
go install github.com/0xsaurabhx/GoPilotKit/cmd/engine@latest

# Or build from source
git clone https://github.com/0xsaurabhx/GoPilotKit.git
cd GoPilotKit
make build

# Create config
cp configs/personal.yaml intelligence.yaml
# Edit intelligence.yaml with your API keys

# Validate config
./bin/gopilotkit validate --config intelligence.yaml

# Run
./bin/gopilotkit run --config intelligence.yaml
```

Then send a message:

```bash
curl -N -X POST http://localhost:8080/run \
  -H "Content-Type: application/json" \
  -d '{"agent": "personal-assistant", "message": "Hello, what can you do?"}'
```

## Features

### 🔄 Three Agentic Protocols

| Protocol | Layer | Status | Implementation |
|----------|-------|--------|----------------|
| **AG-UI** | Agent ↔ User | ✅ Complete | SSE streaming, HITL interrupts, generative UI, state management |
| **MCP** | Agent ↔ Tools | ✅ Complete | JSON-RPC 2.0, stdio transport, tool discovery & execution |
| **A2A** | Agent ↔ Agent | ✅ Complete | Agent Cards, skill definitions, task lifecycle, delegation manager |

### 🧠 Autonomous Self-Improvement

The agent gets smarter from real usage — **zero human intervention required**.

| Capability | Status | Description |
|------------|--------|-------------|
| **Signal Extraction** | ✅ | 8 signal types: positive, negative, correction, implicit, tool retry, clarification |
| **Evaluation** | ✅ | LLM-as-judge scoring (helpfulness, accuracy, safety), regression testing, baselines |
| **Prompt Evolution** | ✅ | Versioned prompts, automatic rewriting from failure patterns, rollback |
| **Few-Shot Curation** | ✅ | TF-IDF similarity retrieval, quality scoring, auto-eviction |
| **Skill Recording** | ✅ | Learns tasks from demonstrations, success rate tracking |
| **Memory Optimization** | ✅ | Strength-based storage, time decay, consolidation, eviction |
| **Container Scoping** | ✅ | Learning isolated per user/org/project/agent |

Every improvement is **auditable** and **traceable** to its source interaction.

### 💾 Persistent Threads

Full-fidelity interaction history that survives restarts:

- SQLite WAL mode with `busy_timeout` for safe concurrent access
- Messages, tool calls, generative UI state
- Conversation history with sliding window
- Thread creation, archival, renaming
- Run records with status tracking and timing

### 🛡️ Enterprise Guardrails

| Guardrail | Status | Description |
|-----------|--------|-------------|
| **PII Scrubbing** | ✅ | Email, phone, SSN, credit card, IP detection and redaction |
| **Content Filtering** | ✅ | Jailbreak detection, prompt injection blocking |
| **Blocked Words** | ✅ | Configurable word/phrase blocking |
| **Input Validation** | ✅ | Pre-LLM content checks |
| **Output Validation** | ✅ | Post-LLM content checks |

### 📊 Product Analytics

| Feature | Status | Description |
|---------|--------|-------------|
| **Event Storage** | ✅ | In-memory event store with configurable retention |
| **Real-time Dashboard** | ✅ | Total runs, active runs, token usage, error rate, hourly buckets |
| **Event Queries** | ✅ | Filter by agent, thread, run, type, time range |
| **OTLP Export** | ✅ | Export events for DataDog, NewRelic, Grafana |
| **Cost Tracking** | ✅ | Per-request, per-user, per-org token usage and cost |

### 💰 Cost Management

| Feature | Status | Description |
|---------|--------|-------------|
| **Token Counting** | ✅ | Prompt, completion, and cached token tracking |
| **Per-Provider Pricing** | ✅ | OpenAI, Anthropic pricing tables with custom overrides |
| **Budget Enforcement** | ✅ | Daily and monthly USD limits |
| **Rate Limiting** | ✅ | Requests per minute and tokens per minute |
| **Usage Summary** | ✅ | Aggregated stats by provider, agent, time period |

### 🔍 Semantic Cache

- TF-IDF vector-based similarity search (no external embedding dependency)
- Cosine similarity matching with configurable threshold
- TTL-based expiration
- Hit counting and cache statistics
- Automatic eviction at capacity

### 📡 Multi-Surface Channels

| Channel | Status | Streaming | Description |
|---------|--------|-----------|-------------|
| **Web** | ✅ | ✅ | Full SSE streaming, rich JSON messages |
| **Slack** | ✅ | ❌ | Event API format, block-based formatting |
| **Teams** | ✅ | ❌ | Bot Framework message format |
| **Mobile** | ✅ | ✅ | Uses Web adapter |

### 🔧 LLM Providers

| Provider | Status | Features |
|----------|--------|----------|
| **OpenAI** | ✅ | GPT-4o, GPT-5, streaming, tool calls |
| **Anthropic** | ✅ | Claude 3.5/4, streaming, tool calls, system prompt extraction |
| **Ollama** | ✅ | Local inference, streaming |
| **Circuit Breaker** | ✅ | Auto-open after 5 failures, 30s reset, half-open recovery |
| **Fallback** | ✅ | Primary → secondary provider with retry |

### 📚 Agentic RAG

- Document ingestion with automatic chunking (configurable size and overlap)
- TF-IDF vector storage and retrieval
- Cosine similarity search with configurable top-K
- Agentic retrieval: the agent decides when retrieval is needed based on query type

### 🔌 WASM Plugins

- Plugin manager with load/invoke/unload lifecycle
- Function discovery and dispatch
- Sandboxed execution (wazero integration ready)

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Client Layer                                  │
│              (Web / Mobile / Slack / Teams)                      │
└────────────────────────────┬────────────────────────────────────┘
                             │  Channel Adapters
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│                  AG-UI SSE Stream Coordinator                    │
│         (Events, HITL Interrupts, State Management)              │
└────────────────────────────┬────────────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────────┐
│                     Runtime Engine                               │
│  ┌──────────┐  ┌────────────┐  ┌──────────┐  ┌──────────────┐ │
│  │  Config   │  │  Provider  │  │ Guardrails│  │    Cost      │ │
│  │  Engine   │  │  Router    │  │  (PII/    │  │   Tracker    │ │
│  │          │  │  (CB/FB)   │  │  Content) │  │  (Budget/RL) │ │
│  └──────────┘  └────────────┘  └──────────┘  └──────────────┘ │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │              Prompt Builder → LLM Stream → Tool Loop      │  │
│  └──────────────────────────┬───────────────────────────────┘  │
│                             │                                   │
│  ┌──────────────────────────▼───────────────────────────────┐  │
│  │          Thread Store (SQLite WAL) + Analytics Store      │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────────┐
│              Autonomous Learning Pipeline                        │
│  Signals → Evaluation → Prompts / Few-Shot / Skills / Memory    │
└─────────────────────────────────────────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────────┐
│  ┌──────────┐  ┌────────────┐  ┌──────────┐  ┌──────────────┐ │
│  │ MCP Host │  │ WASM Host  │  │  Semantic │  │   A2A        │ │
│  │ (stdio)  │  │ (wazero)   │  │   Cache   │  │  Protocol    │ │
│  └──────────┘  └────────────┘  └──────────┘  └──────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

## Configuration

GoPilotKit is configured via a single YAML file:

```yaml
version: "v1alpha"
profile: "personal"  # personal | team | enterprise

server:
  port: 8080
  transport: ["sse"]

storage:
  driver: "sqlite"
  connection_uri: "./data/gopilotkit.db"

providers:
  - id: "primary-llm"
    type: "openai"
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
```

See [`configs/`](configs/) for personal, team, and enterprise examples.

### Environment Variables

Use `${VAR}` or `${VAR|default}` syntax in config files:

```yaml
api_key: "${OPENAI_API_KEY}"
connection_uri: "${DATABASE_URL|postgres://localhost:5432/gopilotkit}"
```

## CLI Commands

```bash
# Start the runtime server
gopilotkit run --config intelligence.yaml

# Validate configuration
gopilotkit validate --config intelligence.yaml

# Print version
gopilotkit version
```

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/run` | Start an agent run (SSE stream) |
| `GET` | `/runs` | List active runs |
| `GET` | `/health` | Health check |

### Example: Start a Run

```bash
curl -N -X POST http://localhost:8080/run \
  -H "Content-Type: application/json" \
  -d '{"agent": "personal-assistant", "message": "Help me with billing"}'
```

### SSE Event Types

| Event | Description |
|-------|-------------|
| `RUN_START` | Run initiated |
| `TEXT_START` / `TEXT_CHUNK` / `TEXT_END` | Streaming text response |
| `TOOL_CALL_START` / `TOOL_CALL_END` | Tool invocation |
| `TOOL_OUTPUT` | Tool result |
| `INTERRUPT_REQUIRED` | Human-in-the-loop pause |
| `STATE_DELTA` / `STATE_SNAPSHOT` | State updates |
| `RUN_END` | Run completed with token usage |
| `RUN_ERROR` | Run failed |

## Binary Size

| Build | Size |
|-------|------|
| Stripped (`-ldflags="-s -w"`) | ~11MB |
| With UPX compression | ~5MB |

Zero CGO dependencies. Cross-compiles anywhere with `CGO_ENABLED=0`.

## Development

```bash
# Build
make build

# Build with debug symbols
make build-dev

# Run all tests
make test

# Run tests with coverage
make test-cover

# Run linter
make lint

# Build + compress
make build-small
```

### Test Coverage

13 packages tested with unit and integration tests:

- `config` — YAML parsing, validation, env var expansion
- `protocol/agui` — SSE encoding, event serialization, handler routing, coordinator
- `provider` — OpenAI/Anthropic/Ollama live server tests, circuit breaker, fallback
- `store/threads` — SQLite CRUD, WAL mode, concurrent access
- `runtime` — Full engine integration with mock LLM servers (streaming, tool calls, persistence, cancellation)
- `guardrails` — PII detection, content filtering, blocked words
- `cost` — Budget enforcement, rate limiting, pricing calculations
- `cache` — Semantic lookup, TTL eviction, hit counting
- `analytics` — Event recording, dashboard, queries
- `channels` — Web/Slack/Teams adapter translation
- `protocol/a2a` — Agent cards, task lifecycle, delegation
- `learning/signals` — Positive/negative/correction signal extraction
- `rag` — Document ingestion, chunking, retrieval

## Project Structure

```
├── cmd/engine/                  # CLI entrypoint (Cobra)
├── internal/
│   ├── config/                  # YAML config, validation, env vars
│   ├── protocol/
│   │   ├── agui/                # AG-UI SSE streaming protocol
│   │   └── a2a/                 # A2A agent-to-agent protocol
│   ├── runtime/                 # Runtime engine (orchestration loop)
│   ├── provider/                # LLM connectors (OpenAI, Anthropic, Ollama)
│   │   └── circuitbreaker.go    # Circuit breaker + fallback provider
│   ├── store/
│   │   ├── threads/             # SQLite thread/message/run store
│   │   └── analytics/           # Analytics event store + dashboard
│   ├── learning/                # Autonomous self-improvement
│   │   ├── signals/             #   Signal extraction
│   │   ├── evaluation/          #   LLM-as-judge, regression testing
│   │   ├── prompts/             #   Prompt evolution
│   │   ├── fewshot/             #   Dynamic few-shot curation
│   │   ├── skills/              #   Skill recording
│   │   ├── memory/              #   Memory optimization
│   │   └── containers/          #   Learning container scoping
│   ├── mcp/                     # MCP host (JSON-RPC 2.0, stdio)
│   ├── wasm/                    # WASM plugin manager
│   ├── rag/                     # Agentic RAG (ingestion + retrieval)
│   ├── guardrails/              # PII scrubbing, content filtering
│   ├── cost/                    # Token counting, budgets, rate limiting
│   ├── cache/                   # Semantic caching (TF-IDF)
│   ├── channels/                # Multi-surface adapters
│   └── enterprise/              # SSO, RBAC, audit (planned)
├── configs/                     # Sample configuration files
├── plugins/                     # Prebuilt WASM plugins
├── Makefile
├── Dockerfile
└── go.mod
```

## Roadmap

- [x] Config engine with YAML validation
- [x] AG-UI SSE streaming protocol
- [x] LLM provider connectors (OpenAI, Anthropic, Ollama)
- [x] Circuit breaker with automatic fallback
- [x] Persistent thread store (SQLite with WAL mode)
- [x] Runtime engine (prompt build → LLM stream → tool loop → persistence)
- [x] Context propagation (client disconnect cancels LLM)
- [x] Autonomous learning engine (7 sub-packages)
- [x] MCP host (JSON-RPC 2.0, stdio transport)
- [x] A2A protocol (agent cards, task lifecycle, delegation)
- [x] Semantic caching (TF-IDF vectors, cosine similarity)
- [x] Guardrails framework (PII scrubbing, content filtering)
- [x] Analytics & OTLP export
- [x] Cost tracking with budget enforcement
- [x] Multi-surface channels (Web, Slack, Teams)
- [x] Agentic RAG (ingestion, chunking, retrieval)
- [x] WASM plugin manager
- [ ] Enterprise SSO/RBAC/Audit
- [ ] Postgres storage driver
- [ ] WebSocket transport
- [ ] gRPC transport
- [ ] Embedding-based vector cache (OpenAI embeddings)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Apache License 2.0 — see [LICENSE](LICENSE).

## Acknowledgments

Built on the shoulders of giants:
- [AG-UI Protocol](https://docs.ag-ui.com/) — Agent-User Interaction Protocol
- [MCP](https://modelcontextprotocol.io/) — Model Context Protocol
- [A2A](https://github.com/a2aproject/A2A) — Agent-to-Agent Protocol
- [CopilotKit](https://www.copilotkit.ai/) — Inspiration for the intelligence platform architecture
