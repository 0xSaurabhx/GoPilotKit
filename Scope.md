## Architectural Blueprint & System Scope

### Core System Overview

The framework operates as a **single-binary runtime bridge** sitting between client surfaces (Web, Mobile, Slack) and agent backends (Native Go pipelines, LangGraph, Python runtimes, or direct LLMs). It provides out-of-the-box durable persistence, continuous learning, and multi-protocol tool invocation driven entirely by a clean YAML declarative manifest.

```
+-------------------------------------------------------------------------+
|                  Client Layer (Web / Mobile / Slack / SDK)             |
+-------------------------------------------------------------------------+
                                    │
                       (AG-UI Protocol / SSE / gRPC)
                                    ▼
+─────────────────────────────────────────────────────────────────────────+
|               GO ENTERPRISE INTELLIGENCE RUNTIME (Binary)               |
|                                                                         |
|  ┌──────────────────┐  ┌────────────────────┐  ┌─────────────────────┐  |
|  │  Config Engine   │  │   Auth & RBAC      │  │  Telemetry/Tracing  │  |
|  │  (YAML / Env)    │  │ (Project/Org Scope)│  │    (OpenTelemetry)  │  |
|  └─────────┬────────┘  └─────────┬──────────┘  └──────────┬──────────┘  |
|            │                     │                        │             |
|  ┌─────────▼─────────────────────▼────────────────────────▼──────────┐  |
|  │               AG-UI / SSE Stream Coordinator                      │  |
|  │  (Text Deltas, Generative UI Payloads, Tool Hooks, Interrupts)    │  |
|  └───────────────────────────────┬───────────────────────────────────┘  |
|                                  │                                      |
|  ┌───────────────────────────────▼───────────────────────────────────┐  |
|  │                  Continuous Self-Improvement Loop                 │  |
|  │  (Implicit Trace Capture -> Semantic Evaluator -> Few-Shot Store) │  |
|  └───────────────────────────────┬───────────────────────────────────┘  |
|                                  │                                      |
|  ┌──────────────────┐  ┌─────────▼──────────┐  ┌─────────────────────┐  |
|  │ Persistence Core │  │  Agent Dispatcher  │  │ Tool & MCP Host     │  |
|  │ SQLite / Postgres│  │ (LLMs/Custom/LangG)│  │ (WASM/Local/Remote) │  |
|  └──────────────────┘  └────────────────────┘  └─────────────────────┘  |
+─────────────────────────────────────────────────────────────────────────+

```

---

### Key Architectural Pillars

#### 1. Universal Agent-UI Protocol Engine (AG-UI Compatible)

* **Typed Event Streaming:** Implements standardized JSON events (`RUN_START`, `TEXT_CHUNK`, `TOOL_CALL`, `GEN_UI_PAYLOAD`, `INTERRUPT_REQUIRED`, `STATE_DELTA`, `RUN_END`) over Server-Sent Events (SSE) and WebSockets.
* **Human-in-the-Loop (HITL):** Thread state suspends execution when a tool requires approval, holding a lightweight Go channel/wait-group before resuming execution upon user input.

#### 2. Declarative Multi-Tenant Scoping

* **Namespace Hierarchy:** `Organization -> Project -> Thread -> Run`.
* **Zero-Code Setup:** All LLMs, MCP servers, embeddings, and telemetry pipelines configure directly in a root `intelligence.yaml`.

#### 3. Continuous Improvement Engine (RLiHF / Trace Indexing)

* **Telemetry Trajectory Tracking:** Automatically logs token sequences, tool execution latencies, validation pass/fails, and implicit user signals (e.g., user rewrites, edits, thumbs-up, tool retries).
* **In-Context Learning Flywheel:** Successful execution traces get indexed into an embedded vector store. When a similar query arrives, the system retrieves high-performing past traces as dynamic dynamic few-shot examples inside prompt context.

#### 4. Unified Tool & MCP Host

* **Model Context Protocol (MCP):** Connects to MCP servers over standard I/O (local sub-processes) or SSE/gRPC (remote services) dynamically.
* **Sandboxed WASM Engine:** Uses `wazero` to execute community plugins (written in Rust, Go, or TypeScript) in-process without granting direct host OS access.

---

### Declarative Configuration Schema (`intelligence.yaml`)

```yaml
version: "v1alpha"
server:
  port: 8080
  transport: ["sse", "grpc"]
  cors_allowed_origins: ["*"]

storage:
  driver: "sqlite" # Supports "sqlite" (embedded) or "postgres"
  connection_uri: "./data/intelligence.db"
  vector_store:
    driver: "embedded" # Embedded cosine similarity or external Qdrant/pgvector
    dimension: 1536

telemetry:
  provider: "opentelemetry"
  endpoint: "http://localhost:4318/v1/traces"
  log_level: "info"

providers:
  - id: "primary-llm"
    type: "openai"
    model: "gpt-4o"
    api_key: "${OPENAI_API_KEY}"
  - id: "backup-llm"
    type: "anthropic"
    model: "claude-3-5-sonnet"
    api_key: "${ANTHROPIC_API_KEY}"

agents:
  - name: "enterprise-support-agent"
    provider: "primary-llm"
    fallback_provider: "backup-llm"
    system_prompt: |
      You are an enterprise support copilot. Ground all factual assertions
      using the tools provided. Never reveal internal schema definitions.
    memory:
      sliding_window: 20
      semantic_recall: true
    continuous_learning:
      enabled: true
      auto_feedback_capture: true
      few_shot_retrieval_k: 3
    tools:
      - "sqlite-db-tool"
      - "mcp-filesystem"

tools:
  - id: "sqlite-db-tool"
    type: "wasm"
    binary_path: "./plugins/db_query.wasm"
  - id: "mcp-filesystem"
    type: "mcp"
    transport: "stdio"
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/data/shared"]

```

---

### Critical Engineering & Production Details

#### Memory Safety & Concurrency

* **Stream Isolation:** Use Go's `sync.Pool` for byte buffers and allocation recycling to prevent garbage collector pauses under heavy SSE streaming traffic.
* **Context Propagation:** Ensure all HTTP/gRPC requests bind strictly to Go's `context.Context` to instantly kill hung upstream LLM connections or runaway sub-processes when a user cancels their query or disconnects.

#### Durable Thread Persistence

* **ACID Transaction Guarantees:** SQLite must run with WAL mode (`PRAGMA journal_mode=WAL;`) and `PRAGMA busy_timeout=5000;` to handle concurrent writes safely across multiple agent threads without database locking panics.
* **Pure-Go SQLite:** Build with `modernc.org/sqlite` rather than `mattn/go-sqlite3` to guarantee zero CGO dependencies for smooth single-binary cross-compilation (`CGO_ENABLED=0`).

#### Enterprise Guardrails & Redaction

* **Zero-Allocation PII Scrubbing:** Stream-level regex and token masking before sending payloads over external provider APIs.
* **Fallback Circuit Breaker:** Wrap LLM clients with rate-limit and error backoffs. If OpenAI returns a `429` or `503`, automatically switch context to Anthropic or a local Ollama instance seamlessly mid-stream.

---

### Modular Folder Structure

```
├── cmd/
│   └── engine/              # Main CLI entrypoint (Cobra)
├── internal/
│   ├── config/              # YAML validation & environment parsing
│   ├── protocol/agui/       # AG-UI event schemas, SSE/gRPC streaming engines
│   ├── runtime/             # Agent orchestrator, context builder, interrupts
│   ├── learning/            # Few-shot vectorization, feedback scoring, trace indexing
│   ├── mcp/                 # Model Context Protocol host & stdio client
│   ├── wasm/                # Sandboxed plugin runtime (wazero engine)
│   ├── store/               # SQL schema, thread persistence (SQLite/Postgres)
│   └── provider/            # LLM connectors (OpenAI, Anthropic, Ollama)
├── plugins/                 # Prebuilt WASM plugins
├── configs/                 # Sample yaml setups
├── Dockerfile               # Multi-stage scratch build producing <30MB binary
└── go.mod

```
