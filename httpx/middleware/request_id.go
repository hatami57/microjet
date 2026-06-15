package middleware

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hatami57/microjet/core"
)

// DefaultRequestIDHeader is the header read and echoed by RequestID. It is the
// canonical correlation-id header shared across MicroJet layers.
const DefaultRequestIDHeader = core.CorrelationIDHeader

// GinRequestIDKey is the gin context key under which the request id is stored.
const GinRequestIDKey = "requestID"

type ctxKey int

const (
	loggerKey ctxKey = iota
)

// RequestIDOption configures the RequestID middleware.
type RequestIDOption func(*requestIDConfig)

type requestIDConfig struct {
	header string
}

// WithRequestIDHeader overrides the header used to read and echo the request id
// (default "X-Request-ID").
func WithRequestIDHeader(header string) RequestIDOption {
	return func(c *requestIDConfig) {
		if header != "" {
			c.header = header
		}
	}
}

// RequestID ensures every request carries a correlation id. It reads the id from
// the request header (generating a UUID when absent), echoes it on the response,
// and stores it on both the gin context and the request's context.Context so
// handlers and services can correlate logs. Install it before Logger.
func RequestID(opts ...RequestIDOption) gin.HandlerFunc {
	cfg := requestIDConfig{header: DefaultRequestIDHeader}
	for _, opt := range opts {
		opt(&cfg)
	}
	return func(c *gin.Context) {
		id := c.GetHeader(cfg.header)
		if id == "" {
			id = uuid.NewString()
		}
		c.Header(cfg.header, id)
		c.Set(GinRequestIDKey, id)
		c.Request = c.Request.WithContext(ContextWithRequestID(c.Request.Context(), id))
		c.Next()
	}
}

// ContextWithRequestID returns a copy of ctx carrying the request id. It
// delegates to core so the id shares one context key across http and messaging.
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return core.ContextWithCorrelationID(ctx, id)
}

// RequestIDFromContext returns the request id stored in ctx, or "" if none.
func RequestIDFromContext(ctx context.Context) string {
	return core.CorrelationIDFromContext(ctx)
}

// ContextWithLogger returns a copy of ctx carrying a request-scoped logger.
func ContextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// LoggerFromContext returns the request-scoped logger stored in ctx, falling
// back to slog.Default() when none is present.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}
