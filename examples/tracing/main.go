// Command tracing demonstrates opt-in distributed tracing (otelx.Module). Adding
// the module installs an OTLP exporter and the W3C trace-context propagator; the
// instrumented layers — the HTTP server and the httpx client — then emit and
// propagate spans automatically, and every request log carries a trace_id for
// log/trace correlation. With tracing off (the default) all of this is a no-op.
//
// Run it:
//
//	go run .
//	curl localhost:8080/work
//
// Watch the logs: each request line includes trace_id=..., and the inbound
// request's trace context is propagated to the outbound httpx call.
//
// To actually view spans, run an OTLP collector on localhost:4318 (e.g. Jaeger:
//
//	docker run --rm -p 16686:16686 -p 4318:4318 jaegertracing/all-in-one
//
// then open http://localhost:16686). The app runs fine without a collector — it
// just cannot deliver spans, so you only see the trace_id correlation in logs.
package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hatami57/microjet/host"
	"github.com/hatami57/microjet/httpx"
	"github.com/hatami57/microjet/otelx"
)

func main() {
	host.MustNew().
		WithModule(otelx.Module()).
		WithModule(httpx.Module()).
		Setup(func(a *host.App) error {
			r := httpx.Of(a).Router

			// A simple downstream the /work handler calls, to show the trace
			// context flowing from one request into the next over HTTP.
			r.GET("/ping", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"pong": true})
			})

			r.GET("/work", func(c *gin.Context) {
				ctx := c.Request.Context()
				// The request-scoped logger carries trace_id; correlate logs to traces.
				log := httpx.LoggerFrom(ctx)
				log.Info("handling work request")

				// The httpx client injects the current trace context into outbound
				// headers, so this call continues the same trace on the server side.
				client := httpx.NewClient("http://"+c.Request.Host, httpx.WithTimeout(2*time.Second))
				var pong map[string]any
				if err := client.GetJSON(ctx, "/ping", &pong); err != nil {
					c.Error(err)
					return
				}

				log.Info("work complete", "downstream", pong)
				c.JSON(http.StatusOK, gin.H{"status": "done", "downstream": pong})
			})
			return nil
		}).
		MustRun()
}
