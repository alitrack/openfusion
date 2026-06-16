# OpenFusion

**Open-source multi-model fusion orchestration engine.** OpenAI-compatible API that dispatches requests to multiple LLMs in parallel and synthesizes the best answer.

> ⚠️ **Research / experimental code.** OpenFusion is a personal research project exploring multi-model fusion patterns. It works in production at the author's environment but comes with no guarantees. Use at your own risk.
>
> 📄 Licensed under MIT — see [LICENSE](LICENSE).
>
> 🌐 [github.com/alitrack/openfusion](https://github.com/alitrack/openfusion)

Like OpenRouter Fusion, but self-hosted on your own infrastructure.

## Features

| Feature | Status |
|---------|--------|
| **OpenAI-compatible API** — drop-in replacement for any LLM client | ✅ |
| **Multi-model parallel dispatch** — goroutine + errgroup, each member has independent timeout | ✅ |
| **Judge synthesis** — reads all panel responses, extracts consensus/contradictions/blind spots, writes final answer | ✅ |
| **Graceful degradation** — panel member failure/timeout → continue with remaining responses | ✅ |
| **Streaming (SSE)** — fusion results streamed as OpenAI-compatible chunks with sentence-boundary flushing | ✅ |
| **Request-level panel/judge override** — inline `panel` and `judge` fields in API request override preset | ✅ |
| **Web Search injection** — Brave Search API (free tier 2K/mo) fetches context, injects into all panel models | ✅ |
| **Skill system** — `.skill.yaml` files define routing strategy (direct / self-ensemble / fusion) with trigger matching | ✅ |
| **MCP Knowledge integration** — MCP client module retrieves domain knowledge from external MCP servers (ChatSQL, OntoMind, knowledge bases) and injects context before panel dispatch | ✅ |
| **Plugin system** — ModelPlugin interface for model-specific optimizations (e.g. DeepSeek think/temperature) | ✅ |
| **Codex mode** — structured code output (language, files, explanation, tests) with `codex: true` | ✅ |
| **Usage metrics & cost dashboard** — web dashboard at `/v1/dashboard`, per-request metrics | ✅ |
| **OpenTelemetry tracing** — every panel/judge step tracked with spans (attributes: preset, model, tokens, cost) | ✅ |
| **Per-preset rate limiting** — token bucket per preset, configurable rate + burst | ✅ |
| **Provider health checks** — unhealthy providers automatically skipped during dispatch | ✅ |
| **Preset CRUD API** — create/delete/list/get presets at runtime via REST API | ✅ |
| **Config hot-reload** — `SIGHUP` triggers atomic engine rebuild without restart | ✅ |
| **Response caching** — LRU + TTL cache with SHA-256 keyed by preset + messages + overrides | ✅ |
| **Adaptive concurrency** — per-provider concurrency limiter with adaptive adjustment | ✅ |
| **OpenRouter gateway plugin** — seamless model routing via OpenRouter's 300+ model catalog | ✅ |

## Quick Start

```bash
# 1. Get a Brave Search API key (free, 2,000 queries/month)
#    https://api.search.brave.com/

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

## Configuration

```yaml
server:
  addr: "127.0.0.1:8080"
  auth_token: "${OPENFUSION_API_KEY}"

providers:
  openrouter:
    base_url: "https://openrouter.ai/api/v1"
    api_key: "${OPENROUTER_API_KEY}"
    plugin: "generic"

presets:
  dir: "presets"
  items:
    budget:
      description: "Cost-effective fusion with 3 diverse models"
      panel:
        - provider: openrouter
          model: deepseek/deepseek-v4-pro
        - provider: openrouter
          model: google/gemini-3-flash
        - provider: openrouter
          model: kimi/kimi-k2.6
      judge:
        provider: openrouter
        model: openai/gpt-5.5-preview

fusion:
  default_timeout: 120
  panel_timeout_per_model: 60
```

## API Reference

### `POST /v1/chat/completions`

Standard OpenAI chat completions format. Set `model` to a preset name (e.g. `openfusion/budget`).

**With request-level overrides:**
```json
{
  "model": "openfusion/budget",
  "messages": [{"role": "user", "content": "Analyze transformer architecture"}],
  "panel": [
    {"provider": "openrouter", "model": "deepseek/deepseek-v4-pro"},
    {"provider": "openrouter", "model": "google/gemini-3-flash"}
  ],
  "judge": {"provider": "openrouter", "model": "openai/gpt-5.5-preview"}
}
```

**With web search injection:**
```json
{
  "model": "openfusion/research",
  "messages": [{"role": "user", "content": "Latest AI papers 2026"}],
  "stream": true
}
```

### Preset Management API

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/v1/presets` | List all presets with details |
| `POST` | `/v1/presets` | Create a new preset |
| `GET` | `/v1/presets/{name}` | Get preset detail |
| `DELETE` | `/v1/presets/{name}` | Delete a preset |

### Other Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/v1/models` | List presets as OpenAI-style models |
| `GET` | `/v1/metrics` | Snapshot of usage/cost metrics |
| `GET` | `/v1/dashboard` | Web dashboard UI |

## Architecture

```
Client → POST /v1/chat/completions {"model":"openfusion/budget"}
          │
          ▼
     API Layer
          │  1. Parse request + auth + validation
          │  2. Apply request-level overrides (panel/judge)
          ▼
     Cache Check (SHA-256 key)
          │  Miss → continue  |  Hit → return cached response
          ▼
     Web Search (if enabled)
          │  Brave Search API → inject context as system message
          ▼
     Panel Dispatch (parallel goroutines)
          │  goroutine 1: DeepSeek V4 Pro
          │  goroutine 2: Gemini 3 Flash
          │  goroutine 3: Kimi K2.6
          │  (graceful: individual timeout/failure doesn't block)
          ▼
     Judge Synthesis
          │  Judge reads all answers
          │  Output: consensus, contradictions, unique insights, blind spots
          │          + synthesized final answer
          ▼
     Return OpenAI-compatible chat.completions JSON
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

## MCP Knowledge Integration

OpenFusion can retrieve domain knowledge from external [MCP (Model Context Protocol)](https://modelcontextprotocol.io) servers before dispatching to panel models. This enables **knowledge-augmented fusion** — panel models receive relevant context from knowledge bases, databases, or ontologies before generating their answers.

### Configuration

Add `mcp_knowledge` sources to any `.skill.yaml` file:

```yaml
# skills/domain-advisor.skill.yaml
strategy:
  mcp_knowledge:
    sources:
      # Stdio mode: spawn a subprocess
      - server_cmd: "python scripts/mcp-knowledge-server.py"
        tool_name: "search_knowledge"
        max_tokens: 4000

      # HTTP mode: connect to a remote MCP server
      - server_url: "http://localhost:8080/mcp"
        tool_name: "Ask"
        max_tokens: 8000
  panel: ...
  judge: ...
```

### Supported Transports

| Transport | Config Field | Use Case |
|:----------|:-------------|:---------|
| **stdio** | `server_cmd` | Local subprocess (Python, Node.js, .NET tools) |
| **HTTP/SSE** | `server_url` | Remote MCP server |

### Example Knowledge Sources

| Source | MCP Tool | Purpose |
|:-------|:---------|:--------|
| [ChatSQL](https://github.com/alitrack/ChatSQL) | `Ask` | Natural language → SQL queries against business databases |
| [OntoMind](https://github.com/alitrack/ChatSQL) | `search_ontology` | Query ontology concepts, entities, and relationships |
| Document knowledge base | `search_knowledge` | RAG retrieval from indexed documents |
| [SwanFlow](https://github.com/alitrack/swanflow) | `RunWorkflow` | Execute and inspect data science workflows |

### Architecture

```
User Question
    │
    ▼
OpenFusion (Skill Matching)
    │
    ├── MCP Client ──→ ChatSQL (NL2SQL query database)
    ├── MCP Client ──→ OntoMind (ontology concept lookup)
    └── MCP Client ──→ Knowledge Base (document RAG)
    │
    ▼ (all context injected into system prompt)
Panel Models (parallel)
    │
    ▼
Judge Synthesis
    │
    ▼
Final Answer
```

See `internal/mcp/` for the MCP client implementation.

## Tests

```bash
go test ./... -v -count=1
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

Full report: `claw/wiki/articles/openfusion-benchmark-2026.md`

## License

MIT
