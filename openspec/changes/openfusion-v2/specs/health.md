# Spec: Provider Health Check

## Behaviour

Each provider is periodically pinged. If it fails N consecutive times, it's marked unhealthy and panel dispatch skips it. One successful ping restores it.

## Config

```yaml
providers:
  modelscope:
    base_url: "https://api-inference.modelscope.cn"
    api_key: "..."
    health_check:
      enabled: true
      interval: 30s
      timeout: 10s
      failure_threshold: 3
      # If empty, uses base_url + /health or performs a minimal chat completion
      endpoint: ""
```

## Implementation

1. Create `internal/health/` package
2. `type Checker struct` — holds `map[string]*ProviderHealth`
3. `ProviderHealth` — `Healthy atomic.Bool`, `failCount atomic.Int64`
4. `Start(ctx, pm, providers map[string]HealthCheckConfig)` — launches goroutine per provider
5. `IsHealthy(name string) bool` — called by panel dispatcher before calling provider
6. Panel dispatch skips unhealthy providers (marks them as error="provider unhealthy")

## Health Check Strategies (tried in order):

1. If `endpoint` set: GET that URL
2. If base URL is an API endpoint: GET base URL (or strip /v1)
3. Fallback: minimal chat completion (1 token) against the provider's cheapest model

## Panel Integration

In `panel.Dispatcher.Dispatch()`:
```go
// Before calling provider, check health
if healthChecker != nil && !healthChecker.IsHealthy(member.Provider) {
    results[idx] = types.PanelResponse{
        Member: member,
        Error: "provider unhealthy (skipped)",
    }
    return
}
```

Unhealthy providers should be logged on startup and when status changes.

## Test Scenarios

- S1: Provider starts healthy → after N failures → unhealthy → panel skips it
- S2: Unhealthy recovers → healthy → panel calls it again
- S3: health_check disabled → no health checks performed
- S4: All providers unhealthy → fusion still returns partial results
