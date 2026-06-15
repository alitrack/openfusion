# Spec: Response Cache

## Behaviour

Cache fusion results per (preset + messages_hash). TTL per preset configurable. LRU eviction when cache full.

## Config

```yaml
cache:
  enabled: false
  max_size: 1000       # LRU max entries
  ttl: 300             # seconds (5 min default)
  presets:             # per-preset override
    budget:
      ttl: 600
    frontier:
      ttl: 1800        # 30 min — expensive calls cached longer
```

## Implementation

1. Create `internal/cache/` package
2. `type FusionCache struct` — wraps `golang-lru/v2` with TTL
3. Key = `preset + SHA256(messages JSON)` — 64 hex chars
4. `Get(key string) (*types.ChatResponse, bool)` — returns cached + hit/miss
5. `Set(key string, resp *types.ChatResponse)`
6. In Engine.Execute: check cache first, store result after completion
7. On cache hit: add `X-Cache: HIT` response header + record metrics hit
8. On cache miss: add `X-Cache: MISS` response header
9. Cache also works for no-judge mode (different key prefix)

## Cache Invalidation

- TTL-based only (no manual invalidation)
- LRU eviction when `max_size` reached

## Dependency

Add `github.com/hashicorp/golang-lru/v2` to go.mod.

## Response Headers

```
X-Cache: HIT    # served from cache
X-Cache: MISS   # computed fresh
Cache-TTL: 300  # remaining TTL in seconds (only on HIT)
```

## Test Scenarios

- S1: Same request twice → second call returns cached (X-Cache: HIT)
- S2: Different messages → no cache collision
- S3: TTL expired → recomputed
- S4: Cache disabled → always MISS
- S5: LRU eviction → oldest entries removed first
