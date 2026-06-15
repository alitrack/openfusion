# OpenFusion

> **Open-source multi-model fusion engine** — single API call, parallel models, judge-synthesized answer.

Inspired by [OpenRouter Fusion](https://openrouter.ai/fusion). OpenFusion brings the same concept to your own infrastructure: one OpenAI-compatible endpoint dispatches your prompt to multiple models in parallel, then a judge model reads all responses and synthesizes one superior answer.

## Why Fusion?

On Perplexity's DRACO deep research benchmark (100 tasks × 10 domains):

| Configuration | Score | Cost |
|---|---|---|
| 🥇 **Fable 5 + GPT-5.5** (Fusion) | **69.0** | $$$ |
| **Gemini 3 Flash + Kimi K2.6 + DeepSeek V4 Pro** (Fusion) | **64.7** | **~50%** of Fable 5 |
| Claude Fable 5 (solo) | 65.3 | $$$ |
| GPT-5.5 (solo) | 60.0 | $$ |
| Opus 4.8 + Opus 4.8 (self-ensemble Fusion) | **65.5** | 2× Opus cost |
| Opus 4.8 (solo) | 58.8 | $$ |

Key finding: **~3/4 of the Fusion gain comes from the synthesis step itself** — even fusing a model with itself yields +6.7 points. For cost-sensitive users, combining affordable models via Fusion often beats paying for a single expensive frontier model.

## Architecture

```
Client → POST /v1/chat/completions {"model":"openfusion/budget"}
         │
         ▼
    API Layer (net/http — std lib, zero frameworks)
         │  1. Parse request + auth check
         │  2. Lookup preset by model name
         │  3. Build FusionRequest from preset + user messages
         ▼
    Fusion Orchestrator
         │  4. Dispatch panel models in parallel (goroutines + errgroup)
         │  5. Collect all responses (graceful on timeout/failure)
         │  6. Build judge prompt (original question + all panel answers)
         │  7. Call judge model → structured analysis
         │     (consensus / contradictions / blind spots / unique insights)
         │  8. Synthesize final answer from analysis
         ▼
    Return standard chat.completions JSON (or SSE stream)
```

### Key design principles

- **OpenAI compatible** — any client (curl, Python SDK, Codex, Claude Code) works with zero migration
- **Single binary** — no external dependencies (Go 1.25+, std lib `net/http`)
- **Graceful degradation** — a single panel model timeout doesn't block the fusion
- **Configurable** — YAML presets define which models and judge to use

## Quick Start

### 1. Install

```bash
# Clone or copy
git clone <your-repo> openfusion
cd openfusion

# Build
go build -o openfusion ./cmd/openfusion/

# (Optional) tests
go test ./... -v
```

### 2. Configure

Copy the example config and set your API keys:

```bash
cp config.example.yaml config.yaml
# Edit config.yaml — at minimum set your API keys
```

Example `config.yaml`:
```yaml
server:
  addr: "127.0.0.1:8080"
  auth_token: ""           # set to require Bearer auth

providers:
  modelscope:
    base_url: "https://api-inference.modelscope.cn"
    api_key: "ms-..."      # or ${MODELSCOPE_API_KEY}

  deepseek:
    base_url: "https://api.deepseek.com"
    api_key: "sk-..."      # or ${DEEPSEEK_API_KEY}

  ollama:
    base_url: "http://localhost:11434"
    api_key: "ollama"

  openrouter:
    base_url: "https://openrouter.ai/api/v1"
    api_key: "${OPENROUTER_API_KEY}"

presets:
  dir: "presets"
```

### 3. Run

```bash
./openfusion -config config.yaml
```

You'll see:
```
  ╔═══════════════════════════════════════╗
  ║         OpenFusion v0.1               ║
  ║   Multi-Model Fusion Engine           ║
  ╚═══════════════════════════════════════╝

  Server:  http://127.0.0.1:8080
  Models:  GET  /v1/models
  Fusion:  POST /v1/chat/completions
```

### 4. Call

```bash
# List available presets
curl http://127.0.0.1:8080/v1/models

# Fusion call
curl -X POST http://127.0.0.1:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "openfusion/budget",
    "messages": [{"role": "user", "content": "Explain quantum computing in simple terms"}]
  }'

# Streaming fusion call
curl -N -X POST http://127.0.0.1:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "openfusion/budget",
    "messages": [{"role": "user", "content": "Explain quantum computing in simple terms"}],
    "stream": true
  }'
```

## Presets

Presets define which models participate in the panel and which model acts as judge. OpenFusion ships with 4 built-in presets:

### Built-in presets

| Model slug | Panel | Judge | Use case |
|---|---|---|---|
| `openfusion/budget` | DeepSeek V4 Pro + Qwen 3.5 27B | GLM-5.1 | Best value: ~50% cost of frontier, near-frontier quality |
| `openfusion/quality` | DeepSeek V4 Pro + Qwen 3.5 122B | GLM-5.1 | Balanced quality/cost |
| `openfusion/frontier` | DeepSeek V4 Pro + Qwen 3.5 122B + Step 3.7 Flash | GLM-5.1 | Maximum diversity |
| `openfusion/self-ensemble` | DeepSeek V4 Pro × 2 runs | GLM-5.1 | Ensemble-only gain (~+6 pts) |

### Writing custom presets

Presets are YAML files in the `presets/` directory. Example:

```yaml
name: my-combo
description: "My custom model combination"
panel:
  - provider: openrouter
    model: anthropic/claude-sonnet-4
    system: "You are a rigorous technical analyst."
  - provider: openrouter
    model: openai/gpt-5.5-preview
    system: "You are a creative thinker."
  - provider: ollama
    model: llama3
judge:
  provider: openrouter
  model: anthropic/claude-opus-4
  system: "Synthesize the best answer from the responses below."
```

You can also define presets inline in `config.yaml` via `presets.items`.

## API Reference

OpenFusion exposes an OpenAI-compatible API. Any client that works with OpenAI works with OpenFusion — just change the `base_url` and `model` name.

### `GET /v1/models`

Returns available fusion presets.

```json
{
  "object": "list",
  "data": [
    {"id": "openfusion/budget", "object": "model", "owned_by": "openfusion"},
    {"id": "openfusion/quality", "object": "model", "owned_by": "openfusion"},
    {"id": "openfusion/frontier", "object": "model", "owned_by": "openfusion"},
    {"id": "openfusion/self-ensemble", "object": "model", "owned_by": "openfusion"}
  ]
}
```

### `POST /v1/chat/completions`

**Request** (OpenAI format):

```json
{
  "model": "openfusion/budget",
  "messages": [{"role": "user", "content": "Your question here"}],
  "stream": false
}
```

**Response** (non-streaming) — standard `chat.completion` object with extra `analysis` field:

```json
{
  "id": "fusion-abc123",
  "object": "chat.completion",
  "model": "openfusion/budget",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "Synthesized answer from all panel models..."
    }
  }],
  "usage": {
    "prompt_tokens": 1523,
    "completion_tokens": 847,
    "total_tokens": 2370,
    "cost_usd": 0.0023
  },
  "analysis": {
    "consensus": ["Point 1 all models agreed on", "Point 2 all models agreed on"],
    "contradictions": [
      {"issue": "Model A said X, Model B said Y",
       "views": {"Model A": "X", "Model B": "Y"}}
    ],
    "partial_coverage": ["Topic only covered by some models"],
    "unique_insights": [
      {"model": "Model A", "insight": "Unique perspective"},
      {"model": "Model B", "insight": "Different angle"}
    ],
    "blind_spots": ["Aspect missed by all panel models"]
  }
}
```

**Response** (streaming `"stream": true`) — SSE format:

```
data: {"type":"analysis","consensus_count":3,"contradictions":1,"blind_spots":1,"unique_insights":2}
data: {"id":"fusion-...","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"S"}}]}
data: {"id":"fusion-...","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"yn"}}]}
...
data: {"type":"usage","prompt_tokens":1523,"completion_tokens":847,"total_tokens":2370,"cost_usd":0.0023}
data: [DONE]
```

### Authentication

If `auth_token` is set in config, all requests must include:
```
Authorization: Bearer <your-token>
```

## Configuration Reference

Full `config.yaml` schema:

```yaml
server:
  addr: "127.0.0.1:8080"           # Listen address
  auth_token: "sk-..."             # API key (empty = no auth)

providers:
  <name>:                           # Arbitrary provider name
    base_url: "https://..."         # API base URL
    api_key: "${ENV_VAR}"           # Inline or env var

presets:
  dir: "presets"                    # Directory with preset YAML files
  items:                            # Inline presets (optional)
    my-preset:
      description: "..."
      panel: [...]
      judge: {...}

fusion:
  default_timeout: 120              # Total fusion timeout (seconds)
  max_concurrent: 8                 # Max parallel panel calls
  panel_timeout_per_model: 60       # Per-model timeout (seconds)
```

Environment variable substitution: `${MY_VAR}` in YAML values is replaced at load time.

## Comparison with OpenRouter Fusion

| Feature | OpenRouter Fusion | OpenFusion |
|---|---|---|
| **Infrastructure** | Cloud (OpenRouter servers) | Self-hosted (your hardware/providers) |
| **Model access** | 300+ models through OpenRouter | Any OpenAI-compatible endpoint |
| **API protocol** | OpenAI-compatible | OpenAI-compatible |
| **Pricing** | Pay per token (OpenRouter markup) | Direct provider costs only |
| **Streaming** | ✔ | ✔ |
| **Custom presets** | Limited | Full YAML control |
| **Latency** | Server-side (optimized network) | Your network latency |
| **Data privacy** | Data leaves your network | Self-hosted, full control |
| **Judge selection** | Auto-selected | Configurable per preset |
| **Cost tracking** | Per-call pricing | Per-model usage breakdown |

## Use Cases

- **Research assistance** — get consensus + blind spot analysis across models
- **Cost optimization** — fuse affordable models instead of paying for premium
- **Ensemble confidence** — detect contradictions and low-confidence areas
- **Self-ensemble** — boost any single model by fusing it with itself (+~6 pts)
- **API aggregation** — unify multiple provider accounts behind one endpoint

## Roadmap

- [x] Core fusion pipeline (panel + judge + synthesis)
- [x] OpenAI-compatible API
- [x] YAML preset system
- [x] Graceful degradation on panel failure
- [x] SSE streaming
- [ ] Usage metrics & cost dashboard
- [ ] OpenTelemetry instrumentation
- [ ] Rate limiting per preset
- [ ] Web UI for preset management
- [ ] Provider health checks

## License

MIT
