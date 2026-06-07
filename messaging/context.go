package messaging

import (
	"context"

	"github.com/hatami57/microjet/core"
)

// ContextWithCorrelationID returns a copy of ctx carrying the correlation id.
// Consumers call this after reading CorrelationIDHeader off an inbound message
// so downstream code (logging, nested calls) can recover the id from context.
// It delegates to core so the id shares one context key across http and messaging.
func ContextWithCorrelationID(ctx context.Context, id string) context.Context {
	return core.ContextWithCorrelationID(ctx, id)
}

// CorrelationIDFromContext returns the correlation id stored in ctx, or "" if
// none is present.
func CorrelationIDFromContext(ctx context.Context) string {
	return core.CorrelationIDFromContext(ctx)
}
