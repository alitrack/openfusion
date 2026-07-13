# OpenFusion

**Open-source multi-model fusion orchestration engine.** OpenAI-compatible API that dispatches requests to multiple LLMs in parallel and synthesizes the best answer. Also supports **Anthropic Messages API** for native integration with Anthropic-protocol agents (forge, Claude Code, etc.) and **DAG task decomposition** for complex multi-step tasks.

> ⚠️ **Research / experimental code.** OpenFusion is a personal research project exploring multi-model fusion patterns. It works in production at the author's environment but comes with no guarantees. Use at your own risk.
>
> 📄 Licensed under MIT — see [LICENSE](LICENSE).
>
> 🌐 [github.com/alitrack/openfusion](https://github.com/alitrack/openfusion)

Like OpenRouter Fusion, but self-hosted on your own infrastructure. Plus DAG scheduling and Anthropic protocol support.

## Features

### Core Fusion Engine

| Feature | Status |
|---------|--------|
| **OpenAI-compatible API** — drop-in replacement for any LLM client (`/v1/chat/completions`) | ✅ |
| **Anthropic Messages API** — native protocol for Anthropic-compatible agents (`/v1/messages`) | ✅ |
| **Multi-model parallel dispatch** — goroutine pool, each member has independent timeout | ✅ |
| **Judge synthesis** — reads all panel responses, extracts consensus/contradictions/blind spots, writes final answer | ✅ |
| **Graceful degradation** — panel member failure/timeout → continue with remaining responses | ✅ |
| **Streaming (SSE)** — fusion results streamed as OpenAI-compatible chunks | ✅ |
| **Request-level panel/judge override** — inline `panel` and `judge` fields in API request override preset | ✅ |

### Task Decomposition (ATG)

| Feature | Status |
|---------|--------|
| **DAG task decomposition** — LLM decomposes complex tasks into atomic subtask graphs | ✅ |
| **Topological parallel execution** — nodes with satisfied dependencies run concurrently | ✅ |
| **Minimal subgraph repair** — on failure, only affected subgraph re-decomposed (max 3 retries) | ✅ |
| **Configurable planner** — choose decomposition LLM via `dag.planner` config section | ✅ |
| **Multi-preset nodes** — different DAG nodes can use different fusion presets (budget/search, quality/analysis) | ✅ |

### Observability & Operations

| Feature | Status |
|---------|--------|
| **Web Search injection** — Brave Search API fetches context, injects into all panel models | ✅ |
| **Skill system** — `.skill.yaml` files define routing strategy with trigger matching + `model: "auto"` | ✅ |
| **MCP Knowledge integration** — retrieve domain knowledge from external MCP servers before dispatch | ✅ |
| **Fusion logging hook** — async CSV logging with monthly rotation + zstd compression | ✅ |
| **Usage metrics & cost dashboard** — web dashboard at `/v1/dashboard` | ✅ |
| **OpenTelemetry tracing** — every panel/judge step tracked with spans | ✅ |
| **Per-preset rate limiting** — token bucket per preset, configurable rate + burst | ✅ |
| **Provider health checks** — unhealthy providers automatically skipped | ✅ |
| **Preset CRUD API** — create/delete/list/get presets at runtime via REST | ✅ |
| **Config hot-reload** — `SIGHUP` triggers atomic engine rebuild without restart | ✅ |
| **Response caching** — LRU + TTL cache with SHA-256 key | ✅ |
| **Adaptive concurrency** — per-provider concurrency limiter | ✅ |

### Provider Ecosystem

| Feature | Status |
|---------|--------|
| **OpenRouter gateway plugin** — 300+ models via single provider | ✅ |
| **Plugin system** — ModelPlugin interface (DeepSeek, Generic, etc.) | ✅ |
| **Codex mode** — structured code output with `codex: true` | ✅ |

## Quick Start

```bash
# 1. Get API keys for your providers
#    - DeepSeek: https://platform.deepseek.com
#    - OpenRouter: https://openrouter.ai
#    - Ollama (local): no key needed

# 2. Configure
cp config.example.yaml config.yaml
# Edit config.yaml: add providers, API keys, presets

# 3. Build
go build -o openfusion ./cmd/openfusion/

# 4. Run
./openfusion -config config.yaml

# 5. Test
curl http://localhost:8080/v1/models
curl http://localhost:8080/v1/dashboard
```

## API Reference

### Core Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/v1/chat/completions` | OpenAI-compatible fusion (standard) |
| `POST` | `/v1/messages` | **Anthropic Messages API** — for Anthropic-protocol agents |
| `GET` | `/v1/models` | List presets as OpenAI-style models |
| `GET` | `/v1/metrics` | Snapshot of usage/cost metrics |
| `GET` | `/v1/dashboard` | Web dashboard UI |

### Preset Management

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/v1/presets` | List all presets with details |
| `POST` | `/v1/presets` | Create a new preset |
| `GET` | `/v1/presets/{name}` | Get preset detail |
| `DELETE` | `/v1/presets/{name}` | Delete a preset |

### Model Routing

| Model | Behavior |
|-------|----------|
| `openfusion/budget` | Budget fusion (cost-optimized panel + judge) |
| `openfusion/quality` | Quality fusion (larger panel + better judge) |
| `openfusion/dag` or `dag` | **DAG mode**: task decomposition → parallel exec → repair |
| `openfusion/auto` or `auto` | Skill-based auto-routing |

### `POST /v1/chat/completions` (OpenAI)

```json
{
  "model": "openfusion/budget",
  "messages": [{"role": "user", "content": "Analyze transformer architecture"}],
  "panel": [
    {"provider": "deepseek", "model": "deepseek-chat"},
    {"provider": "deepseek", "model": "deepseek-v4-flash"}
  ],
  "judge": {"provider": "deepseek", "model": "deepseek-chat"}
}
```

### `POST /v1/messages` (Anthropic — for forge, Claude Code, etc.)

```json
{
  "model": "budget",
  "max_tokens": 1024,
  "messages": [{"role": "user", "content": "Explain quantum computing"}]
}
```

Response is standard Anthropic Messages format:
```json
{
  "id": "msg_...",
  "type": "message",
  "role": "assistant",
  "model": "budget",
  "content": [{"type": "text", "text": "..."}],
  "stop_reason": "end_turn",
  "usage": {"input_tokens": 12, "output_tokens": 569}
}
```

### DAG Mode (Anthropic or OpenAI)

```bash
# OpenAI format
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"dag","messages":[{"role":"user","content":"Search latest AI papers, summarize top 3, and write a blog post"}]}'

# Anthropic format (same, just different endpoint)
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"dag","max_tokens":4096,"messages":[{"role":"user","content":"Search latest AI papers, summarize top 3, and write a blog post"}]}'
```

**How DAG works:**
1. Planner LLM decomposes task → DAG of atomic subtasks
2. Executor runs nodes topologically, parallel where dependencies allow
3. Each node calls full fusion pipeline (panel → judge) using its assigned preset
4. On failure: minimal subgraph repair (freezes verified nodes, re-decomposes only failed region)
5. Final answer synthesized from the last topological node

## Configuration

```yaml
server:
  addr: "127.0.0.1:8080"
  auth_token: "${OPENFUSION_API_KEY}"

providers:
  deepseek:
    base_url: "https://api.deepseek.com"
    api_key: "${DEEPSEEK_API_KEY}"
  ollama:
    base_url: "http://localhost:11434"
    api_key: "ollama"

presets:
  dir: "presets"
  items:
    budget:
      description: "Cost-effective fusion with 2 models"
      panel:
        - provider: deepseek
          model: deepseek-chat
        - provider: deepseek
          model: deepseek-v4-flash
      judge:
        provider: deepseek
        model: deepseek-chat

dag:
  planner:
    provider: deepseek           # LLM used for task decomposition
    model: deepseek-chat
    max_tokens: 4096

fusion:
  default_timeout: 120
  panel_timeout_per_model: 60

logging:
  hook:
    enabled: true
    output_dir: "fusion_log"
    auto_split: monthly
```

## Architecture

```
Client → POST /v1/chat/completions {"model":"openfusion/budget"}
              or POST /v1/messages (Anthropic protocol)
          │
          ▼
     API Layer (OpenAI + Anthropic dual protocol)
          │  1. Parse request + auth + validation
          │  2. Translate protocol (Anthropic → internal format)
          │  3. Apply request-level overrides (panel/judge)
          ▼
     Cache Check (SHA-256 key)
          │  Miss → continue  |  Hit → return cached response
          ▼
     Web Search (if enabled) → Brave Search API → inject context
          ▼
     Panel Dispatch (parallel goroutines)
          │  goroutine 1: Model A
          │  goroutine 2: Model B
          │  goroutine 3: Model C
          │  (graceful: individual timeout/failure doesn't block)
          ▼
     Judge Synthesis
          │  Judge reads all answers
          │  Output: consensus, contradictions, unique insights, blind spots
          │          + synthesized final answer
          ▼
     Return response in original protocol format
     (OpenAI chat.completions or Anthropic messages)
```

### DAG Mode Architecture

```
Client → {"model":"dag","messages":[...]}
          │
          ▼
     Planner (LLM decompose)
          │  Task → DAG of atomic subtasks
          │  {nodes: [{id, description, preset, prompt}], edges: [["1","2"]]}
          ▼
     DAG Executor (topological parallel dispatch)
          │  ┌─ Node 1: preset=budget  → panel+judge  ┐
          │  ├─ Node 2: preset=quality → panel+judge  │ parallel (no deps)
          │  ├─ Node 3: preset=coding  → panel+judge  │ (depends on 1,2)
          │  └─ Node 4: preset=budget  → panel+judge  │ (depends on 3)
          ▼
     Repair (on failure: minimal subgraph rebuild, up to 3 attempts)
          │  Verified nodes frozen, only affected subgraph re-decomposed
          ▼
     Final fused answer
```

## Skills

Skills define routing strategies via `.skill.yaml` files. They use trigger matching for automatic routing.

```yaml
# skills/code-review.skill.yaml
name: code-review
description: "Multi-model code review with consensus analysis"
triggers:
  - categories: "code|review"
    min_tokens: 200
mode: fusion
strategy:
  panel:
    - provider: openrouter
      model: anthropic/claude-sonnet-4
      system: "You are a code review expert."
    - provider: openrouter
      model: openai/gpt-5.5-preview
      system: "Focus on security and performance."
  judge:
    provider: openrouter
    model: openai/gpt-5.5-preview
    enabled: true
```

Set `model: "auto"` or `model: "openfusion/auto"` to use skill matching.

## Forge Integration

OpenFusion natively supports forge (the Go AI coding agent) via the Anthropic Messages API:

```bash
# Configure forge to use OpenFusion
export ANTHROPIC_BASE_URL="http://localhost:8080/v1"
export ANTHROPIC_API_KEY="noop"

# Forge now calls OpenFusion transparently
forge --model budget        # Standard multi-model fusion
forge --model dag           # ATG task decomposition
forge --model auto          # Skill-based auto-routing
```

Each forge agent step benefits from multi-model consensus, and complex multi-step tasks can leverage DAG decomposition.

## MCP Knowledge Integration

OpenFusion can retrieve domain knowledge from external [MCP (Model Context Protocol)](https://modelcontextprotocol.io) servers before dispatching to panel models.

```yaml
# skills/domain-advisor.skill.yaml
strategy:
  mcp_knowledge:
    sources:
      - server_cmd: "python scripts/mcp-knowledge-server.py"
        tool_name: "search_knowledge"
        max_tokens: 4000
      - server_url: "http://localhost:8080/mcp"
        tool_name: "Ask"
        max_tokens: 8000
```

## Tests

```bash
go test ./internal/... -v
```

## Benchmark

Validated against 3 tasks × 7 modes with DS V4 Pro blind judge:

| Mode | Avg Score | Cost/req |
|---|---|---|
| Opus 4.6 (single) | **89.7** | ~$0.60 |
| **Budget (DS Pro + Flash)** | **86.0** | **~$0.05** |
| Flash self-ensemble | 85.7 | ~$0.03 |
| DS Pro self-ensemble | 83.3 | ~$0.08 |
| DS Pro single | 81.3 | ~$0.04 |

**Key finding**: Budget fusion (DS V4 Pro + Flash at ~$0.05) beats Opus 4.6 on code tasks (88 vs 85) at 1/12 the cost.

## License

MIT
