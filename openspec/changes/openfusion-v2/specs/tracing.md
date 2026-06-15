# Spec: OpenTelemetry Tracing

## Behaviour

When `OTEL_EXPORTER_OTLP_ENDPOINT` env var is set, OpenFusion initializes the OTel SDK and creates traced spans for each fusion request. Otherwise, tracing is a no-op (zero overhead, zero imports).

## Span Structure

```
Fusion.Execute                                       [root]
  ├── panel.call [model=deepseek-ai/DeepSeek-V4-Pro]  [child of root]
  ├── panel.call [model=Qwen/Qwen3.5-27B]             [child of root]
  └── judge.synthesize [model=ZhipuAI/GLM-5.1]        [child of root]
```

## Span Attributes

| Span | Attributes |
|---|---|
| `Fusion.Execute` | `openfusion.preset`, `openfusion.panel_count`, `openfusion.judge_model`, `openfusion.total_duration_ms` |
| `panel.call` | `openfusion.provider`, `openfusion.model`, `openfusion.duration_ms`, `openfusion.token_count`, `openfusion.cost_usd`, `openfusion.success` |
| `judge.synthesize` | `openfusion.provider`, `openfusion.model`, `openfusion.duration_ms`, `openfusion.token_count`, `openfusion.cost_usd` |

## Implementation

1. Create `internal/tracing/` package with helper:
   ```go
   type Tracer struct {
       tracer otel.Tracer
       enabled bool
   }
   func NewTracer() *Tracer  // checks OTEL_EXPORTER_OTLP_ENDPOINT
   func (t *Tracer) StartSpan(ctx, name, attrs...) (context.Context, Span)
   ```
2. The package exports `noopTracer` when OTel is not available — no compile-time dependency on OTel SDK when disabled
3. Update `fusion.Engine.Execute` to create spans
4. Update `panel.Dispatch` to create child spans
5. Update `judge.Synthesize` to create child span
6. Update `fusion.NewEngine()` to accept a `*tracing.Tracer`

## OTEL Dependencies

Only go mod depends when OTel is enabled:
```
go.opentelemetry.io/otel v1.34.0
go.opentelemetry.io/otel/sdk v1.34.0
go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.34.0
go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.34.0
```

## Test Scenarios

- S1: No env → tracing disabled, zero span overhead
- S2: With OTEL endpoint → valid spans created
- S3: Spans carry correct attributes (preset, model, duration)
- S4: Trace context propagates through HTTP headers (W3C traceparent)
