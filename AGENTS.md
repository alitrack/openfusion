# AGENTS.md — OpenFusion

OpenFusion 是一个 Go 编写的多模型编排引擎。对外暴露 OpenAI 兼容的 `/v1/chat/completions` 端点，一次调用并行分发到多个模型，Judge 综合后返回最优答案。

对标 OpenRouter Fusion，但开源、可自托管。

## Build & Run

```bash
# Build
go build -o openfusion ./cmd/openfusion/

# Run (config.yaml must exist)
./openfusion -config config.yaml

# Test
go test ./internal/... -v
```

## Architecture

```
Client → POST /v1/chat/completions {"model":"openfusion/budget"}
          │
          ▼
     API Layer (net/http)
          │  1. Parse request + auth
          │  2. Lookup preset by model name
          │  3. Create FusionRequest from preset + user messages
          ▼
     Fusion Orchestrator
          │  4. Dispatch to panel models (parallel goroutines)
          │  5. Collect all responses (graceful on failure)
          │  6. Build judge prompt (original question + all answers)
          │  7. Call judge model → structured analysis
          │  8. Synthesize final answer
          ▼
     Return standard chat.completions JSON
```

## Package Layout

```
cmd/openfusion/main.go    — Entry point
internal/
├── api/                  — HTTP handlers + router
├── config/               — YAML config loader (+ env var substitution)
├── types/                — Shared data types
├── preset/               — Panel+Judge preset registry
├── provider/             — Provider interface + adapters (OpenAI, OpenRouter, Anthropic)
├── panel/                — Parallel dispatch orchestrator
└── judge/                — Judge prompt builder + executor
```

## Key Patterns

- **Provider interface**: Every adapter implements `ChatCompletion(ctx, *types.ChatRequest) → *types.ChatResponse, error`
- **Concurrency**: goroutine + errgroup for parallel panel dispatch; each member has independent timeout
- **Graceful degradation**: panel member timeout/failure → continue with remaining responses
- **Preset = model name**: `openfusion/budget`, `openfusion/quality` etc. map to YAML-defined panel+judge combos
- **OpenAI compatible**: request/response formats match OpenAI chat.completions exactly

## Risk Gates

- AUTO-APPROVED: read/search files, run tests, build, small Go struct additions
- REQUIRES APPROVAL: external HTTP calls (provider APIs), file system writes outside project, config changes, go.mod dependency adds

## Test Convention

- Table-driven tests preferred
- Config/preset tests use temp files (no test fixtures)
- E2E tests use mock HTTP server (no real API calls in unit tests)
- `go test ./internal/... -v` must pass before commit

## Commit Convention

```
type(scope): description

- Bullet changes
```

Types: `feat`, `fix`, `chore`, `docs`, `test`, `refactor`, `perf`
Scopes: `api`, `config`, `types`, `preset`, `provider`, `panel`, `judge`, `fusion`
