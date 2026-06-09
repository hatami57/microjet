package core

import "context"

// CorrelationIDHeader is the canonical header carrying a correlation (request)
// id across process boundaries — HTTP requests and message broker frames alike.
// httpx and messaging both read and write this header so an id flows uniformly
// http -> messaging -> http.
const CorrelationIDHeader = "X-Request-ID"

type correlationIDKey struct{}

// ContextWithCorrelationID returns a copy of ctx carrying the correlation id.
// It is the single source of truth for the correlation-id context key, shared
// by every microjet layer so the id survives across http and messaging hops.
func ContextWithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey{}, id)
}

// CorrelationIDFromContext returns the correlation id stored in ctx, or "" if
// none is present.
func CorrelationIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(correlationIDKey{}).(string)
	return id
}
