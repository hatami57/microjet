package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
)

// tracerName identifies this instrumentation in exported spans.
const tracerName = "github.com/hatami57/microjet/httpx"

// Tracing starts a server span for every request, continuing a W3C trace
// carried in the incoming headers (traceparent) and exposing the span via the
// request context. Without a global tracer provider (see the otelx module) it
// degrades to a no-op, so the server installs it unconditionally. Install it
// after RequestID and before Logger so logs can carry the trace id.
func Tracing() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := otel.GetTextMapPropagator().Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))

		// Name spans by route pattern, not raw path, to keep cardinality bounded;
		// unmatched requests (404s) fall back to the bare method.
		spanName := c.Request.Method
		attrs := []trace.SpanStartOption{
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPRequestMethodKey.String(c.Request.Method),
				semconv.URLPath(c.Request.URL.Path),
			),
		}
		if route := c.FullPath(); route != "" {
			spanName += " " + route
			attrs = append(attrs, trace.WithAttributes(semconv.HTTPRoute(route)))
		}

		ctx, span := otel.Tracer(tracerName).Start(ctx, spanName, attrs...)
		defer span.End()
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		status := c.Writer.Status()
		span.SetAttributes(semconv.HTTPResponseStatusCode(status))
		if status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(status))
		}
	}
}
