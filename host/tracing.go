package host

import (
	"github.com/hatami57/microjet/otelx"
)

// WithTracing registers the OpenTelemetry tracing service. Config is loaded
// from the [tracing] section (enabled by default, exporting to an OTLP/HTTP
// collector at localhost:4318); service name and version default to the [app]
// section. Once registered, the instrumented microjet layers — HTTP server and
// client, GORM, NATS — emit and propagate spans automatically; without it they
// remain no-ops. Pending spans are flushed on shutdown.
func (a *App) WithTracing() *App {
	if a.err != nil {
		return a
	}
	t := otelx.New()
	t.SetLogger(a.Logger)
	t.SetServiceInfo(a.Config.App.Name, a.Config.App.Version)
	ProvideService(a, t)
	return a
}
