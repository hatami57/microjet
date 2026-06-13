package messaging

import (
	"context"

	"github.com/hatami57/microjet/core"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// InjectContext returns headers enriched with the cross-process metadata
// carried by ctx: the W3C trace context (traceparent/tracestate, via the global
// OpenTelemetry propagator — a no-op until one is installed, e.g. by the otelx
// module) and the correlation id. An existing correlation header is preserved.
// The (possibly newly allocated) map is returned so callers can write:
//
//	msg.Headers = messaging.InjectContext(ctx, msg.Headers)
func InjectContext(ctx context.Context, headers map[string][]string) map[string][]string {
	if headers == nil {
		headers = map[string][]string{}
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(headers))
	if id := core.CorrelationIDFromContext(ctx); id != "" && CorrelationID(headers) == "" {
		headers = SetCorrelationID(headers, id)
	}
	return headers
}

// ExtractContext returns ctx enriched with the metadata carried in headers:
// the remote W3C trace context and the correlation id. Broker clients call it
// before invoking a handler so logging and nested calls continue the trace.
func ExtractContext(ctx context.Context, headers map[string][]string) context.Context {
	if len(headers) == 0 {
		return ctx
	}
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(headers))
	if id := CorrelationID(headers); id != "" {
		ctx = core.ContextWithCorrelationID(ctx, id)
	}
	return ctx
}
