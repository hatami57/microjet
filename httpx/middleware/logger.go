package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

func Logger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// Derive a request-scoped logger tagged with the correlation id (set by
		// the RequestID middleware, if installed) and expose it to handlers via
		// the request context so their logs correlate automatically.
		reqLogger := logger
		if id := RequestIDFromContext(c.Request.Context()); id != "" {
			reqLogger = logger.With("request_id", id)
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

		switch {
		case status >= 500:
			reqLogger.LogAttrs(c.Request.Context(), slog.LevelError, "Server error", attrs...)
		case status >= 400:
			reqLogger.LogAttrs(c.Request.Context(), slog.LevelWarn, "Client error", attrs...)
		default:
			reqLogger.LogAttrs(c.Request.Context(), slog.LevelInfo, "Success", attrs...)
		}
	}
}
