# Design: OpenFusion v2 — Architecture Decisions

---

## ADR-1: 用量统计用内存计数器 + `/v1/metrics` 端点，不引入 Prometheus 依赖

**Decision**: 在内存中维护计数器（`sync.Map` + `atomic`），直接通过 `GET /v1/metrics` 以 JSON 格式暴露。不引入 Prometheus client 库，不要求部署 Prometheus Server。

**Alternatives considered**:
- Prometheus 客户端 + `/metrics` 端点 — 标准做法但引入外部依赖，且需要 Prometheus 生态才可用
- Structured logging + 外部摄取 — 不便于实时查询
- SQLite — 太重，OpenFusion 定位为无状态代理

**Rationale**: 
- OpenFusion 是轻量无状态代理，不是监控系统
- 每一步（panel dispatch）本身就有唯一 ID，可以按 preset + panel member 聚合
- 用户如果需要持久化，可以用外部脚本轮询 `/v1/metrics`
- Go `atomic` 操作零开销，`sync.Map` 适配 preset granularity 的动态 key

**Key metrics to track**:
- `fusion_requests_total{preset}` — 调用次数
- `fusion_requests_in_flight{preset}` — 当前并发
- `fusion_duration_ms{preset,quantile}` — 延迟分布（p50/p90/p99）
- `fusion_token_cost{preset}` — 累计成本 USD
- `fusion_panel_success{preset,model}` — panel 成功/失败计数
- `fusion_cache_hit{preset}` — 缓存命中率（仅缓存在线时）

**Implementation**:
```
internal/metrics/
├── metrics.go        — Collector + Recorder struct
├── metrics_test.go   — 单元测试
└── recorder.go       — 实际记录方法
```

---

## ADR-2: No-Judge 模式通过请求参数控制，非独立端点

**Decision**: 在 `ChatRequest` 中加 `judge` 字段（默认 `true`）。`"judge": false` 时跳过 judge 步骤，返回 panel 原始回答数组。

**Alternatives considered**:
- 独立 model slug `openfusion/budget/no-judge` — 导致 preset 数量翻倍
- 独立 endpoint `/v1/fusion/panel` — 增加 API surface

**Rationale**: 
- 参数方式零侵入，与 OpenAI 兼容格式一致
- 用户端只需加一个 JSON 字段
- 后端只需在 Fusion Engine 中加一个分支：if !judge → return panel only

**Response format change** (when judge=false):
```json
{
  "id": "fusion-...",
  "object": "chat.completion",
  "model": "openfusion/budget",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "Raw concatenation of all panel responses..."
    }
  }],
  "panel_responses": [
    {"model": "deepseek/...", "content": "...", "usage": {...}},
    {"model": "qwen/...", "content": "...", "usage": {...}}
  ],
  "usage": {...}
}
```

The `content` field is a concatenation of all panel responses with model labels as a convenience. Clients should use `panel_responses` for structured access.

---

## ADR-3: Rate Limiting 用 token bucket 算法，per-preset 粒度

**Decision**: 每个 preset 独立 token bucket，支持 `rate`（每秒请求数）和 `burst`（峰值突发）配置。使用标准 `golang.org/x/time/rate` 库。

**Alternatives considered**:
- 全局限流 — 一个 client 打满影响其他 preset
- Per-IP — 需要状态管理，且不适合 API 使用场景
- Sliding window — 实现更复杂，token bucket 足够

**Rationale**:
- Per-preset 粒度最合理：`openfusion/budget` 用量大不限，`openfusion/frontier` 贵要限
- Token bucket 是业界标准算法，实现简单
- `golang.org/x/time/rate` 是 Go 标准准库扩展，成熟稳定

**Config extension**:
```yaml
rate_limit:
  enabled: false
  default:               # 所有 preset 的默认值
    rate: 10
    burst: 20
  presets:               # 按 preset 覆盖
    frontier:
      rate: 2
      burst: 5
```

---

## ADR-4: Provider Health Check 用定时 PING 模式，不依赖模型调用

**Decision**: 每个 provider 加配置 `health_check_endpoint`，健康检查发 GET 到该端点（如有）或定期调一次 `chat.completions`（最小请求 1 token）。失败 N 次后标记为 unhealthy，从路由中摘除。

**Alternatives considered**:
- 每次调用前健康检查 — 增加延迟，且高频轮询无意义
- 被动检测（仅标记失败）— 无法预先发现问题

**Rationale**:
- ModelScope / DeepSeek / Ollama 都有 health endpoint 或至少返回 200
- 健康状态用 `atomic` 标记，panel dispatch 前检查
- 失败 N 次（可配置，默认 3）后摘除，成功 1 次后恢复
- 无需从 Manager 中删除 provider，只做运行时路由跳过

**Config extension**:
```yaml
providers:
  modelscope:
    base_url: "..."
    api_key: "..."
    health_check:
      enabled: true
      interval: 30s        # 检查间隔
      failure_threshold: 3  # 连续失败 N 次标记 unhealthy
```

---

## ADR-5: 响应缓存用内存 LRU，TTL 按 preset 配置

**Decision**: 使用 `github.com/hashicorp/golang-lru/v2` 的 LRU 缓存，key 为 `preset + messages_hash（SHA256）`，value 为完整 `ChatResponse`。TTL 可配置。

**Alternatives considered**:
- Redis — 引入外部依赖，不符合单 binary 原则
- 简单 map — 无淘汰机制，内存泄漏
- 文件缓存 — 慢，且缓存数据量不大

**Rationale**:
- LRU 自动淘汰最久未访问的条目，防止内存无限增长
- `golang-lru/v2` 是成熟库，带 TTL 支持
- Key 用 SHA256 哈希避免超长 key 在 map 中浪费内存
- TTL 典型值：高质量回答（Fusion）缓存 5-10 分钟

**Response header**: 缓存命中时加 `X-Cache: HIT` 响应头。

**Config extension**:
```yaml
cache:
  enabled: false
  max_size: 1000           # LRU 最大条目数
  ttl: 300s                # TTL in seconds
  presets:
    budget:
      ttl: 600s
```

---

## ADR-6: OpenTelemetry tracing 用 `go.opentelemetry.io/otel` 最小集成

**Decision**: 在 Engine 层插入 trace span，覆盖 panel dispatch → judge → response 三段。通过环境变量 `OTEL_EXPORTER_OTLP_ENDPOINT` 控制是否启用（无 endpoint = 不初始化 tracing）。

**Alternatives considered**:
- 自己写 tracing — 不如 OTel 标准
- Datadog/Prometheus 专有格式 — 锁定供应商
- 不做 tracing — 生产环境无法排查性能问题

**Rationale**:
- OTel 是 CNCF 标准，支持任意后端（Jaeger、Datadog、Grafana Tempo）
- 环境变量控制开闭，零配置启动
- 只插入关键 span（panel call、judge call、total fusion），不过度埋点
- trace 中携带 preset name、model name、token count 等 attributes

**Span structure**:
```
fusion.Execute (root span, preset=budget)
├── panel.ModelA (call to deepseek/DeepSeek-V4-Pro)
├── panel.ModelB (call to Qwen/Qwen3.5-27B)
└── judge.synthesize (call to judge model)
```

**Dependency**: only when `OTEL_EXPORTER_OTLP_ENDPOINT` is set, import OTel SDK.
