package host

import (
	"context"
	"errors"

	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/httpx"
)

// WithHTTPServer creates the HTTP server and runs the optional setup handlers
// (typically route registration). It registers default readiness probes and
// adds the server to the service container so the host's lifecycle (Init/Close)
// is managed automatically. Errors are deferred to Run/MustRun/Err.
func (a *App) WithHTTPServer(setup ...HandlerFunc) *App {
	if a.err != nil {
		return a
	}
	a.HTTPServer = httpx.NewServer(httpx.ServerConfig{Debug: a.Config.App.Debug}, a.Logger)

	a.registerDefaultReadinessChecks()

	// Route-registration handlers are queued, not run now: they run after services
	// are initialized (so a.DB() is connected) but before the server starts serving.
	for _, s := range setup {
		a.Setup(s)
	}

	ProvideService(a, a.HTTPServer)
	return a
}

// registerDefaultReadinessChecks wires a /readyz probe that, at request time,
// asks every container service implementing healthChecker whether it is ready.
// Ranging at request time means services registered before or after
// WithHTTPServer are both covered; each service returns a self-describing error.
func (a *App) registerDefaultReadinessChecks() {
	a.HTTPServer.AddReadinessCheck("services", func(ctx context.Context) error {
		var errs error
		a.container.Range(func(_, v any) bool {
			if hc, ok := v.(core.HealthChecker); ok {
				if err := hc.Healthy(ctx); err != nil {
					errs = errors.Join(errs, err)
				}
			}
			return true
		})
		return errs
	})
}
