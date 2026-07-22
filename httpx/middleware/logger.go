package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hatami57/microjet/core/logx"
	"go.opentelemetry.io/otel/trace"
)

// Logger logs one access-log line per request. minLevel optionally raises the
// floor for those lines (see ServerConfig.LogLevel): "warn" logs only 4xx/5xx,
// "error" only 5xx. It gates just the access-log line — the request-scoped logger
// seeded into the context for handlers is never filtered.
func Logger(logger *slog.Logger, minLevel string) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// Derive a request-scoped logger tagged with the correlation id (set by
		// the RequestID middleware, if installed) and expose it to handlers via
		// the request context so their logs correlate automatically.
		reqLogger := logger
		if id := RequestIDFromContext(c.Request.Context()); id != "" {
			reqLogger = logger.With("request_id", id)
		}
		// Tag logs with the trace id (set by the Tracing middleware, if a tracer
		// provider is installed) so log lines join up with exported spans.
		if sc := trace.SpanContextFromContext(c.Request.Context()); sc.HasTraceID() {
			reqLogger = reqLogger.With("trace_id", sc.TraceID().String())
		}
		c.Request = c.Request.WithContext(ContextWithLogger(c.Request.Context(), reqLogger))

		c.Next()

		latency := time.Since(start).Microseconds()
		status := c.Writer.Status()
		path := c.Request.URL.Path
		if q := c.Request.URL.RawQuery; q != "" {
			path = path + "?" + q
		}

		attrs := []slog.Attr{
			slog.Int("status", status),
			slog.Int64("latency", latency),
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.String("ip", c.ClientIP()),
			slog.String("user-agent", c.Request.UserAgent()),
		}

		// Gate only the access-log line by the component level; the context
		// logger handlers use stays unfiltered.
		emitLogger := logx.WithMinLevel(reqLogger, minLevel)
		switch {
		case status >= 500:
			emitLogger.LogAttrs(c.Request.Context(), slog.LevelError, "Server error", attrs...)
		case status >= 400:
			emitLogger.LogAttrs(c.Request.Context(), slog.LevelWarn, "Client error", attrs...)
		default:
			emitLogger.LogAttrs(c.Request.Context(), slog.LevelInfo, "Success", attrs...)
		}
	}
}
