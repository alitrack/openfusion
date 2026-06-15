# Spec: Rate Limiting

## Algorithm

Token bucket (per preset): `rate` tokens/sec replenished, `burst` max capacity.

## Config

```yaml
rate_limit:
  enabled: false
  default:
    rate: 10
    burst: 20
  presets:
    frontier:
      rate: 2
      burst: 5
    self-ensemble:
      rate: 5
      burst: 10
```

## Response on Rate Limited

HTTP 429 + JSON body:

```json
{
  "error": "rate limit exceeded for preset 'budget'",
  "retry_after_seconds": 2.5
}
```

Header: `Retry-After: 2` (seconds, integer)

## Implementation

1. Create `internal/ratelimit/` package
2. `NewLimiter(cfg RateLimitConfig, presetNames []string) *Limiter`
3. `Allow(presetName string) (bool, time.Duration)` — returns if allowed + how long to wait
4. Middleware or check in `handleChatCompletions` before executing fusion
5. Add config fields to `config.Config`

## Dependency

Add `golang.org/x/time/rate` to go.mod.

## Test Scenarios

- S1: First 10 calls pass, 11th is rate limited (default rate=10)
- S2: Different presets have independent rate limiters
- S3: rate_limit.enabled=false → no rate limiting applied
- S4: burst allows short spikes
