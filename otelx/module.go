package otelx

import (
	"github.com/hatami57/microjet/host"
)

// Module registers the OpenTelemetry tracing service. Config is loaded from the
// [tracing] section (enabled by default, exporting to an OTLP/HTTP collector at
// localhost:4318); service name and version default to the [app] section. Once
// installed, the instrumented MicroJet layers — HTTP server and client, GORM,
// NATS — emit and propagate spans automatically; without it they remain no-ops.
// Pending spans are flushed on shutdown.
//
// Tracing configures the global OpenTelemetry provider, so it is effectively a
// singleton; the optional name exists only for API symmetry with the other
// modules and to retrieve the service via otelx.Of(app, name).
func Module(name ...string) host.Module {
	return host.ModuleFunc(func(app *host.App) error {
		t := New()
		t.SetLogger(app.Logger)
		t.SetServiceInfo(app.Config.App.Name, app.Config.App.Version)
		host.ProvideService(app, t, name...)
		return nil
	})
}

// ShutdownOrder closes tracing late (as a backend) so spans emitted while other
// services shut down still have a live exporter; the final flush happens last.
func (t *Tracing) ShutdownOrder() int { return host.ShutdownBackend }

// Of returns the tracing service installed by Module under the optional name,
// panicking if none was installed.
func Of(app *host.App, name ...string) *Tracing {
	return host.MustResolveService[*Tracing](app, name...)
}
