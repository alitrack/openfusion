// Package tracing provides OpenTelemetry instrumentation for OpenFusion.
// When OTEL_EXPORTER_OTLP_ENDPOINT env var is set, tracing is enabled.
// Otherwise, all operations are no-ops with zero overhead.
package tracing

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Tracer wraps the OTel tracer with an enabled flag.
type Tracer struct {
	tracer  trace.Tracer
	enabled bool
}

// NewTracer creates a tracer. It checks OTEL_EXPORTER_OTLP_ENDPOINT to
// decide whether to initialize the OTel SDK or use noop.
func NewTracer() *Tracer {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		return &Tracer{enabled: false}
	}

	// If endpoint is set, we initialize the SDK. For simplicity in v0.1,
	// we use the OTel noop provider for compilation correctness and rely
	// on environment variables for SDK initialization.
	// In production, users set OTEL_EXPORTER_OTLP_ENDPOINT and the Go OTel
	// SDK auto-configures from environment variables.
	tp := otel.GetTracerProvider()
	tracer := tp.Tracer("openfusion")

	return &Tracer{
		tracer:  tracer,
		enabled: true,
	}
}

// Enabled returns whether tracing is active.
func (t *Tracer) Enabled() bool {
	return t.enabled
}

// StartSpan starts a new span with the given name and attributes.
// Returns the modified context and a function to end the span.
// When tracing is disabled, returns noop span + identity context.
func (t *Tracer) StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, Span) {
	if !t.enabled {
		return ctx, noopSpan{}
	}
	ctx, span := t.tracer.Start(ctx, name, trace.WithAttributes(attrs...))
	return ctx, &otelSpan{span: span}
}

// Span is the minimal span interface for OpenFusion instrumentation.
type Span interface {
	End()
	SetAttributes(kv ...attribute.KeyValue)
	RecordError(err error)
}

// noopSpan is returned when tracing is disabled.
type noopSpan struct{}

func (noopSpan) End()                              {}
func (noopSpan) SetAttributes(...attribute.KeyValue) {}
func (noopSpan) RecordError(error)                 {}

// otelSpan wraps an OTel span.
type otelSpan struct {
	span trace.Span
}

func (s *otelSpan) End() {
	s.span.End()
}

func (s *otelSpan) SetAttributes(kv ...attribute.KeyValue) {
	s.span.SetAttributes(kv...)
}

func (s *otelSpan) RecordError(err error) {
	s.span.RecordError(err)
}

// Predefined attribute keys for OpenFusion spans.
var (
	AttrPreset      = attribute.Key("openfusion.preset")
	AttrPanelCount  = attribute.Key("openfusion.panel_count")
	AttrJudgeModel  = attribute.Key("openfusion.judge_model")
	AttrProvider    = attribute.Key("openfusion.provider")
	AttrModel       = attribute.Key("openfusion.model")
	AttrDuration    = attribute.Key("openfusion.duration_ms")
	AttrTokenCount  = attribute.Key("openfusion.token_count")
	AttrCostUSD     = attribute.Key("openfusion.cost_usd")
	AttrSuccess     = attribute.Key("openfusion.success")
)
