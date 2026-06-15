# Tasks: OpenFusion v2 — Production-Ready Features

> Total: **23 tasks** | **Estimated: 5 sessions** | Priority order: A → E → C → D → B → F

---

## Session 1: Metrics & Cost Dashboard (Priority A)

**Goal**: `/v1/metrics` endpoint with per-preset and per-model breakdown

- [ ] **1.1 Create `internal/metrics/` package**
  - `Collector` struct with `sync.Map` for per-preset counters
  - `Recorder` methods: `RecordRequest(preset)`, `RecordPanelCall(preset, model, duration, tokens, cost, success)`, `RecordFusionComplete(preset, duration)`
  - Thread-safe via `atomic.Int64` + `sync.Map`
  - Duration quantiles: track all durations in a sorted slice (reset periodically)

- [ ] **1.2 Wire Collector into Engine + Panel**
  - `fusion.NewEngine()` accepts `*metrics.Collector`
  - `Engine.Execute()` calls `RecordRequest` before + `RecordFusionComplete` after
  - `panel.Dispatch()` calls `RecordPanelCall` per member

- [ ] **1.3 Add `GET /v1/metrics` endpoint**
  - Handler formats Collector data as JSON
  - Return uptime, per-preset stats, per-model breakdown
  - In-flight counter (inc on start, dec on complete)

- [ ] **1.4 Add config field + test**
  - `metrics.enabled: true` in config.yaml
  - Test: verify metrics accumulate correctly after N calls
  - Test: concurrent safety (goroutine race test)

---

## Session 2: No-Judge Mode (Priority E)

**Goal**: `"judge": false` skips synthesis, returns raw panel responses

- [ ] **2.1 Add `Judge bool` field to `ChatRequest`** (default true)
  - `json:"judge,omitempty"` — only serializes when false

- [ ] **2.2 Modify `Engine.Execute()`**
  - If `!req.Judge`: skip judge step, build response from panel responses directly
  - `content` = concatenation of all panel responses with `=== model ===` headers
  - Add `PanelResponses []PanelResponseSummary` to `ChatResponse`
  - `PanelResponseSummary`: model, content, duration_ms, token counts

- [ ] **2.3 Tests**
  - Test: judge=false returns all panel responses (none lost)
  - Test: judge=false response format matches spec
  - Test: judge=true (default) unchanged behavior

---

## Session 3: Rate Limiting (Priority C)

**Goal**: Per-preset token bucket rate limiting

- [ ] **3.1 Create `internal/ratelimit/` package**
  - `Limiter` struct wrapping `golang.org/x/time/rate`
  - `NewLimiter(cfg, presetNames)` — creates per-preset limiters
  - `Allow(preset) (bool, retryAfter)` — thread-safe

- [ ] **3.2 Add config fields**
  - `rate_limit.enabled`, `rate_limit.default.rate`, `rate_limit.default.burst`, `rate_limit.presets.<name>`

- [ ] **3.3 Wire into API handler**
  - Before `engine.Execute()`: check rate limiter
  - If exceeded: return HTTP 429 + `Retry-After` header + error JSON

- [ ] **3.4 Add `golang.org/x/time/rate` dependency + tests**
  - Test: exact rate limit boundary
  - Test: per-preset independence
  - Test: disabled = passthrough

---

## Session 4: Provider Health Check + Response Cache (Priority D + B)

**Goal**: Health checker + LRU cache (can be done in parallel since they're independent)

- [ ] **4.1 Create `internal/health/` package**
  - `Checker` struct with `map[string]*ProviderHealth`
  - `Start(ctx, pm, configs)` — goroutine per provider
  - `IsHealthy(name) bool` — called by panel dispatcher
  - Health check strategies: endpoint GET, base GET, minimal chat completion
  - Config: `enabled`, `interval`, `failure_threshold`, `timeout`

- [ ] **4.2 Wire health check into panel dispatch**
  - `Dispatcher.Dispatch()` checks `IsHealthy()` before calling provider
  - Unhealthy → mark as error="provider unhealthy (skipped)"
  - Log health status transitions

- [ ] **4.3 Create `internal/cache/` package**
  - `FusionCache` wrapping `hashicorp/golang-lru/v2`
  - Key = `SHA256(preset + messages JSON)` + `judge` flag
  - `Get(key) (*ChatResponse, bool)` + `Set(key, *ChatResponse)`

- [ ] **4.4 Wire cache into Engine.Execute**
  - Check cache before panel dispatch
  - Store result in cache after completion
  - Set `X-Cache: HIT/MISS` + `Cache-TTL` response headers

- [ ] **4.5 Add dependencies + config + tests**
  - `go get github.com/hashicorp/golang-lru/v2`
  - Config: `cache.enabled`, `cache.max_size`, `cache.ttl`, `cache.presets`
  - Test: cache hit/miss, TTL expiry, LRU eviction, mutual exclusion
  - Test: health check unhealthy → panel skip → recovery

---

## Session 5: OpenTelemetry Tracing (Priority F)

**Goal**: OTel tracing with zero-overhead when disabled

- [ ] **5.1 Create `internal/tracing/` package**
  - Conditional import (build tag or runtime check)
  - `NewTracer() *Tracer` — checks `OTEL_EXPORTER_OTLP_ENDPOINT`
  - `StartSpan(ctx, name, attrs...)` — returns `(ctx, span)`
  - Noop implementation when OTel disabled (zero imports)

- [ ] **5.2 Instrument Engine.Execute + Panel + Judge**
  - Root span: `Fusion.Execute` with preset/panel_count/judge_model attributes
  - Child spans for each panel call
  - Child span for judge synthesis
  - Add W3C traceparent propagation through HTTP headers

- [ ] **5.3 Wire Tracer into Engine**
  - `fusion.NewEngine()` accepts `*tracing.Tracer`
  - Pass context through the call chain

- [ ] **5.4 Add OTel dependencies + test**
  - `go get go.opentelemetry.io/otel ...`
  - Test: spans created when enabled
  - Test: zero overhead when disabled
  - Test: correct attributes on each span

---

## Summary

| Session | Pri | Feature | Tasks | Dependencies |
|---|---|---|---|---|
| 1 | A | Metrics & Cost Dashboard | 4 | Engine + Panel |
| 2 | E | No-Judge Mode | 3 | ChatRequest |
| 3 | C | Rate Limiting | 4 | Config |
| 4 | D+B | Health Check + Cache | 5 | Config, Panel, Engine |
| 5 | F | OpenTelemetry Tracing | 4 | Engine, Panel, Judge |

**Total: 23 tasks | 5 sessions**

## Dependencies

- Session 1: needs Engine reference (already exists)
- Session 2: needs ChatRequest change (no deps)
- Session 3: needs Config extension (no deps)
- Session 4: Health needs Panel + Config; Cache needs Engine + Config
- Session 5: needs Engine + Panel + Judge interfaces

Sessions 1, 2, 3 are independent and could run in any order. Session 4 depends on Config changes (but those can be added ad-hoc). Session 5 is last because it touches the most files.
