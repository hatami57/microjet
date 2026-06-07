package core

import "context"

// CorrelationIDHeader is the canonical header / message-attribute key for
// propagating a correlation (request) ID across HTTP and messaging layers.
const CorrelationIDHeader = "X-Correlation-ID"

type correlationIDKey struct{}

// ContextWithCorrelationID returns a copy of ctx carrying id.
func ContextWithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey{}, id)
}

// CorrelationIDFromContext returns the correlation ID stored in ctx, or "" if
// none was set.
func CorrelationIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(correlationIDKey{}).(string)
	return id
}
