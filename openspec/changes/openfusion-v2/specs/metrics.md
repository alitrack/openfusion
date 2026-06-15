# Spec: Metrics & Cost Dashboard

## Endpoint

`GET /v1/metrics` — Prometheus 兼容 + JSON 格式

## Response Format

```json
{
  "uptime_seconds": 84723,
  "fusion": {
    "total_requests": 1542,
    "requests_in_flight": 3,
    "total_cost_usd": 12.47,
    "avg_duration_ms": 4820
  },
  "presets": {
    "budget": {
      "requests": 892,
      "success": 876,
      "failed": 16,
      "total_cost_usd": 2.13,
      "avg_duration_ms": 3450,
      "p50_duration_ms": 2800,
      "p90_duration_ms": 6200,
      "p99_duration_ms": 12000,
      "panel_models": {
        "deepseek-ai/DeepSeek-V4-Pro": {
          "calls": 892,
          "success": 885,
          "avg_duration_ms": 2100,
          "total_tokens": 142000,
          "total_cost_usd": 0.89
        },
        "Qwen/Qwen3.5-27B": {
          "calls": 892,
          "success": 876,
          "avg_duration_ms": 3400,
          "total_tokens": 198000,
          "total_cost_usd": 0.74
        }
      }
    }
  }
}
```

## Collector Interface

```go
// internal/metrics/metrics.go
type Collector struct {
    startTime    time.Time
    requests     sync.Map  // preset -> *presetMetrics
    inFlight     atomic.Int64
}
```

## Config Changes

```yaml
metrics:
  enabled: true
```

## Implementation Plan

1. Create `internal/metrics/` package with `Collector` struct
2. Wire into Engine: Engine holds reference to Collector
3. `panel.Dispatch` records per-model stats
4. `Engine.Execute` records per-preset stats + duration
5. API handler exposes `GET /v1/metrics` (no auth by default, or same auth as other endpoints)
6. Add metrics field to `Config` struct
7. Register handler in `Server.registerRoutes()`

## Test Scenarios

- S1: After 1 fusion call, metrics show 1 request, >0 cost, >0 duration
- S2: In-flight counter inc/dec correctly (concurrent calls)
- S3: Panel model breakdown shows correct model names
- S4: `/v1/metrics` returns valid JSON even when empty
