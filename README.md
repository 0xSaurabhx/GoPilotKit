# GoPilotKit

<p align="center">
  <strong>Enterprise Intelligence Runtime — Single Binary, All Protocols</strong>
</p>

<p align="center">
  <a href="#quickstart">Quick Start</a> •
  <a href="#features">Features</a> •
  <a href="#architecture">Architecture</a> •
  <a href="#configuration">Configuration</a> •
  <a href="#contributing">Contributing</a> •
  <a href="#license">License</a>
</p>

---

GoPilotKit is a **single Go binary** enterprise intelligence platform implementing all three agentic protocols — **AG-UI**, **MCP**, and **A2A** — with autonomous scaffold-based self-improvement, persistent threads, and analytics.

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

## Features

### 🔄 Three Agentic Protocols

| Protocol | Layer | Purpose |
|----------|-------|---------|
| **AG-UI** | Agent ↔ User | Real-time SSE streaming, HITL interrupts, generative UI |
| **MCP** | Agent ↔ Tools | 2026-07-28 spec, stateless core, MCP Apps |
| **A2A** | Agent ↔ Agent | Agent Cards, multi-agent delegation, secure collaboration |

### 🧠 Autonomous Self-Improvement

The agent gets smarter from real usage — **zero human intervention required**.

- **Prompt Evolution** — Automatically rewrites system prompts on failure patterns
- **Few-Shot Curation** — Dynamically selects the best examples for each query type
- **Skill Recording** — Learns tasks from user demonstrations
- **Memory Optimization** — Prunes and consolidates semantic memory

Every improvement is **auditable** and **traceable** to its source interaction.

### 💾 Persistent Threads

Full-fidelity interaction history that survives restarts and works across devices:

- Messages, tool calls, generative UI state
- Human-in-the-loop approvals
- Voice transcripts, file uploads, multimodal attachments
- Auto-naming, archival, cross-device sync

### 📊 Product Analytics

- Real-time dashboards
- Cost tracking per request/user/organization
- OTLP export to DataDog, NewRelic, Grafana
- SQL-queryable interaction data

### 🏢 Tiered Profiles

| Feature | Personal | Team | Enterprise |
|---------|----------|------|------------|
| Core runtime | ✅ | ✅ | ✅ |
| SQLite persistence | ✅ | ✅ | — |
| Postgres persistence | — | Optional | ✅ |
| A2A protocol | — | ✅ | ✅ |
| SSO / RBAC / Audit | — | — | ✅ |
| Full analytics | — | ✅ | ✅ |

### 🔧 LLM Providers

- **OpenAI** — GPT-4o, GPT-5, etc.
- **Anthropic** — Claude 3.5, Claude 4, etc.
- **Ollama** — Local inference (Llama, Mistral, etc.)
- **Circuit Breaker** — Automatic fallback on provider errors

### 🛡️ Enterprise Guardrails

- PII scrubbing and content filtering
- Token-based rate limiting
- Budget controls per org/project
- Input/output validation

## Architecture

```
┌─────────────────────────────────────────────────────┐
│              Client Layer                            │
│         (Web / Mobile / Slack / Teams)               │
└──────────────────────┬──────────────────────────────┘
                       │  AG-UI Protocol / SSE / gRPC
                       ▼
┌─────────────────────────────────────────────────────┐
│           GoPilotKit Runtime (Single Binary)         │
│                                                      │
│  ┌──────────┐  ┌──────────┐  ┌──────────────────┐  │
│  │  Config   │  │  Auth &  │  │   Telemetry &    │  │
│  │  Engine   │  │  RBAC    │  │   Observability  │  │
│  └─────┬────┘  └─────┬────┘  └────────┬─────────┘  │
│        │             │                 │             │
│  ┌─────▼─────────────▼─────────────────▼──────────┐ │
│  │         AG-UI / SSE Stream Coordinator          │ │
│  └──────────────────────┬──────────────────────────┘ │
│                         │                            │
│  ┌──────────────────────▼──────────────────────────┐ │
│  │       Continuous Self-Improvement Loop          │ │
│  │  (Trace Capture → Eval → Few-Shot → Prompts)    │ │
│  └──────────────────────┬──────────────────────────┘ │
│                         │                            │
│  ┌──────────┐  ┌────────▼────────┐  ┌────────────┐ │
│  │ SQLite / │  │ Agent Dispatcher │  │ Tool Host  │ │
│  │ Postgres │  │ (Graph Runtime)  │  │ MCP + WASM │ │
│  └──────────┘  └─────────────────┘  └────────────┘ │
└─────────────────────────────────────────────────────┘
```

## Configuration

GoPilotKit is configured via a single YAML file:

```yaml
version: "v1alpha"
profile: "enterprise"  # personal | team | enterprise

server:
  port: 8080
  transport: ["sse", "grpc"]

storage:
  driver: "postgres"
  connection_uri: "${DATABASE_URL}"

providers:
  - id: "primary-llm"
    type: "openai"
    model: "gpt-4o"
    api_key: "${OPENAI_API_KEY}"

agents:
  - name: "support-agent"
    provider: "primary-llm"
    system_prompt: |
      You are an enterprise support copilot.
```

See [`configs/`](configs/) for full examples.

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
  -d '{"agent": "support-agent", "message": "Help me with billing"}'
```

## Binary Size

GoPilotKit targets minimal binary size:

| Build | Size |
|-------|------|
| Stripped (`-ldflags="-s -w"`) | ~7MB |
| With UPX compression | ~3MB |

## Development

```bash
# Build
make build

# Build with debug symbols
make build-dev

# Run tests
make test

# Run tests with coverage
make test-cover

# Run linter
make lint

# Build + compress
make build-small
```

## Project Structure

```
├── cmd/engine/              # CLI entrypoint (Cobra)
├── internal/
│   ├── config/              # YAML config, validation, env vars
│   ├── protocol/
│   │   ├── agui/            # AG-UI SSE streaming protocol
│   │   └── a2a/             # A2A agent-to-agent protocol
│   ├── runtime/             # Graph-based execution engine
│   ├── learning/            # Autonomous self-improvement
│   │   ├── signals/         #   Signal extraction
│   │   ├── evaluation/      #   LLM-as-judge, regression
│   │   ├── prompts/         #   Prompt evolution
│   │   ├── fewshot/         #   Dynamic few-shot curation
│   │   ├── skills/          #   Skill recording
│   │   ├── memory/          #   Memory optimization
│   │   └── containers/      #   Learning container scoping
│   ├── rag/                 # Agentic RAG
│   ├── mcp/                 # MCP host (2026-07-28)
│   ├── wasm/                # Sandboxed WASM plugins
│   ├── store/
│   │   ├── threads/         # SQLite/Postgres thread store
│   │   └── analytics/       # Analytics data store
│   ├── provider/            # LLM connectors + fallback
│   ├── cost/                # Token counting, budgets
│   ├── cache/               # Semantic caching
│   ├── guardrails/          # Input/output validation
│   ├── channels/            # Multi-surface adapters
│   └── enterprise/          # SSO, RBAC, audit
├── configs/                 # Sample configuration files
├── plugins/                 # Prebuilt WASM plugins
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
- [ ] Graph-based runtime engine (DAG execution)
- [ ] Autonomous learning engine
- [ ] MCP host (2026-07-28 spec)
- [ ] A2A protocol
- [ ] Semantic caching
- [ ] Guardrails framework
- [ ] Analytics & OTLP export
- [ ] Multi-surface channels (Slack, Teams)
- [ ] Enterprise SSO/RBAC/Audit
- [ ] Agentic RAG

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
