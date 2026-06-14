package messaging

import (
	"context"
	"testing"

	"github.com/hatami57/microjet/core"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func withW3CPropagator(t *testing.T) {
	t.Helper()
	prev := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(prev) })
}

func sampledContext(t *testing.T) (context.Context, trace.SpanContext) {
	t.Helper()
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		SpanID:     trace.SpanID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		TraceFlags: trace.FlagsSampled,
	})
	return trace.ContextWithSpanContext(context.Background(), sc), sc
}

func TestInjectExtractRoundTrip(t *testing.T) {
	withW3CPropagator(t)
	ctx, sc := sampledContext(t)
	ctx = core.ContextWithCorrelationID(ctx, "corr-1")

	headers := InjectContext(ctx, nil)
	if len(headers) == 0 {
		t.Fatal("expected headers to be populated")
	}
	if got := CorrelationID(headers); got != "corr-1" {
		t.Errorf("correlation id header = %q", got)
	}

	out := ExtractContext(context.Background(), headers)
	gotSC := trace.SpanContextFromContext(out)
	if gotSC.TraceID() != sc.TraceID() {
		t.Errorf("trace id = %s, want %s", gotSC.TraceID(), sc.TraceID())
	}
	if !gotSC.IsRemote() {
		t.Error("extracted span context should be remote")
	}
	if got := core.CorrelationIDFromContext(out); got != "corr-1" {
		t.Errorf("correlation id from ctx = %q", got)
	}
}

func TestInjectPreservesExistingCorrelationHeader(t *testing.T) {
	ctx := core.ContextWithCorrelationID(context.Background(), "from-ctx")
	headers := SetCorrelationID(nil, "already-set")
	headers = InjectContext(ctx, headers)
	if got := CorrelationID(headers); got != "already-set" {
		t.Errorf("correlation id = %q, want already-set", got)
	}
}

func TestExtractEmptyHeadersReturnsSameContext(t *testing.T) {
	ctx := context.Background()
	if got := ExtractContext(ctx, nil); got != ctx {
		t.Error("expected identical context for empty headers")
	}
}

func TestInjectWithoutPropagatorOnlyAddsCorrelation(t *testing.T) {
	// Default (no-op) propagator: only the correlation id should appear.
	ctx := core.ContextWithCorrelationID(context.Background(), "corr-2")
	headers := InjectContext(ctx, nil)
	if got := CorrelationID(headers); got != "corr-2" {
		t.Errorf("correlation id = %q", got)
	}
	if len(headers) != 1 {
		t.Errorf("expected only the correlation header, got %v", headers)
	}
}
