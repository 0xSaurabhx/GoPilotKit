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
| AG-UI | Agent <-> User | SSE streaming, HITL interrupts, generative UI, state management |
| MCP | Agent <-> Tools | JSON-RPC 2.0, stdio transport, tool discovery & execution |
| A2A | Agent <-> Agent | Agent Cards, skill definitions, task lifecycle, delegation |

## Self-Improvement Engine

The agent learns from real usage without human intervention.

| Capability | Description |
|------------|-------------|
| Signal Extraction | 8 signal types: positive, negative, correction, implicit, tool retry, clarification |
| Evaluation | LLM-as-judge scoring (helpfulness, accuracy, safety), regression testing, baselines |
| Prompt Evolution | Versioned prompts, automatic rewriting from failure patterns, rollback |
| Few-Shot Curation | TF-IDF similarity retrieval, quality scoring, auto-eviction |
| Skill Recording | Learns tasks from demonstrations, success rate tracking |
| Memory Optimization | Strength-based storage, time decay, consolidation, eviction |
| Container Scoping | Learning isolated per user/org/project/agent |

All improvements are auditable and traceable to their source interaction.

## Storage

SQLite WAL mode with `busy_timeout` for safe concurrent access. Stores messages, tool calls, generative UI state, conversation history with sliding window, thread creation/archival/renaming, and run records with status tracking.

## Guardrails

| Guardrail | Description |
|-----------|-------------|
| PII Scrubbing | Email, phone, SSN, credit card, IP detection and redaction |
| Content Filtering | Jailbreak detection, prompt injection blocking |
| Blocked Words | Configurable word/phrase blocking |
| Input Validation | Pre-LLM content checks |
| Output Validation | Post-LLM content checks |

## Analytics

In-memory event store with configurable retention. Real-time dashboard (total runs, active runs, token usage, error rate, hourly buckets). Event queries filterable by agent, thread, run, type, and time range. OTLP export for DataDog, NewRelic, and Grafana. Per-request, per-user, per-org cost tracking.

## Cost Management

Token counting (prompt, completion, cached). Per-provider pricing tables (OpenAI, Anthropic) with custom overrides. Daily and monthly budget enforcement. Rate limiting by requests/minute and tokens/minute. Aggregated usage summaries by provider, agent, and time period.

## Caching

TF-IDF vector-based similarity search with no external embedding dependency. Cosine similarity matching with configurable threshold. TTL-based expiration, hit counting, automatic eviction at capacity.

## Channels

| Channel | Streaming | Notes |
|---------|-----------|-------|
| Web | Yes | Full SSE streaming, rich JSON messages |
| Slack | No | Event API format, block-based formatting |
| Teams | No | Bot Framework message format |
| Mobile | Yes | Uses Web adapter |

## LLM Providers

| Provider | Notes |
|----------|-------|
| OpenAI | GPT-4o, GPT-5, streaming, tool calls |
| Anthropic | Claude 3.5/4, streaming, tool calls, system prompt extraction |
| Ollama | Local inference, streaming |

Includes circuit breaker (auto-open after 5 failures, 30s reset, half-open recovery) and primary/secondary provider fallback with retry.

## RAG

Document ingestion with automatic chunking (configurable size and overlap). TF-IDF vector storage and retrieval with cosine similarity search. The agent decides when retrieval is needed based on query type.

## WASM Plugins

Plugin manager with load/invoke/unload lifecycle. Function discovery and dispatch. Sandboxed execution via wazero.

## Configuration

Single YAML file:

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

Environment variables use `${VAR}` or `${VAR|default}` syntax:

```yaml
api_key: "${OPENAI_API_KEY}"
connection_uri: "${DATABASE_URL|postgres://localhost:5432/gopilotkit}"
```

## CLI

```bash
gopilotkit run --config intelligence.yaml      # Start the runtime server
gopilotkit validate --config intelligence.yaml # Validate configuration
gopilotkit version                              # Print version
```

## API

| Method | Path | Description |
|--------|------|-------------|
| POST | /run | Start an agent run (SSE stream) |
| GET | /runs | List active runs |
| GET | /health | Health check |

### SSE Events

`RUN_START`, `TEXT_START`, `TEXT_CHUNK`, `TEXT_END`, `TOOL_CALL_START`, `TOOL_CALL_END`, `TOOL_OUTPUT`, `INTERRUPT_REQUIRED`, `STATE_DELTA`, `STATE_SNAPSHOT`, `RUN_END`, `RUN_ERROR`

## Build

| Target | Command | Output |
|--------|---------|--------|
| Production | `make build` | Stripped binary (~11MB) |
| Debug | `make build-dev` | With debug symbols |
| Compressed | `make build-small` | UPX compressed (~5MB) |
| Tests | `make test` | All packages |
| Coverage | `make test-cover` | With coverage report |
| Lint | `make lint` | Static analysis |

Cross-compiles with `CGO_ENABLED=0`.

## Architecture

```
Client Layer (Web / Mobile / Slack / Teams)
       |
  Channel Adapters
       v
AG-UI SSE Stream Coordinator
       |
  Runtime Engine
  - Config Engine, Provider Router (CB/FB), Guardrails, Cost Tracker
  - Prompt Builder -> LLM Stream -> Tool Loop
  - Thread Store (SQLite WAL) + Analytics Store
       |
  Autonomous Learning Pipeline
  Signals -> Evaluation -> Prompts / Few-Shot / Skills / Memory
       |
  MCP Host, WASM Host, Semantic Cache, A2A Protocol
```

## Project Structure

```
cmd/engine/                  CLI entrypoint (Cobra)
internal/
  config/                    YAML config, validation, env vars
  protocol/agui/             AG-UI SSE streaming protocol
  protocol/a2a/              A2A agent-to-agent protocol
  runtime/                   Runtime engine (orchestration loop)
  provider/                  LLM connectors (OpenAI, Anthropic, Ollama)
  store/threads/             SQLite thread/message/run store
  store/analytics/           Analytics event store + dashboard
  learning/                  Autonomous self-improvement
  mcp/                       MCP host (JSON-RPC 2.0, stdio)
  wasm/                      WASM plugin manager
  rag/                       Agentic RAG (ingestion + retrieval)
  guardrails/                PII scrubbing, content filtering
  cost/                      Token counting, budgets, rate limiting
  cache/                     Semantic caching (TF-IDF)
  channels/                  Multi-surface adapters
configs/                     Sample configuration files
plugins/                     Prebuilt WASM plugins
```

## Roadmap

- [x] Enterprise SSO/RBAC/Audit
- [ ] Postgres storage driver
- [ ] WebSocket transport
- [ ] gRPC transport
- [ ] Embedding-based vector cache (OpenAI embeddings)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Apache License 2.0. See [LICENSE](LICENSE).

## Acknowledgments

Built on [AG-UI Protocol](https://docs.ag-ui.com/), [MCP](https://modelcontextprotocol.io/), [A2A](https://github.com/a2aproject/A2A), and [CopilotKit](https://www.copilotkit.ai/).
