package host

import (
	"context"
	"fmt"

	"github.com/hatami57/microjet/httpx"
	"github.com/hatami57/microjet/messaging"
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

// registerDefaultReadinessChecks wires /readyz probes that read App state at
// request time, so databases or messaging registered before or after
// WithHTTPServer are both covered.
func (a *App) registerDefaultReadinessChecks() {
	a.HTTPServer.AddReadinessCheck("database", func(ctx context.Context) error {
		var checkErr error
		a.container.Range(func(_, v any) bool {
			svc, ok := v.(*databaseService)
			if !ok || svc.db == nil {
				return true
			}
			sqlDB, err := svc.db.DB()
			if err != nil {
				checkErr = fmt.Errorf("db %q: %w", svc.name, err)
				return false
			}
			if err := sqlDB.PingContext(ctx); err != nil {
				checkErr = fmt.Errorf("db %q: %w", svc.name, err)
				return false
			}
			return true
		})
		return checkErr
	})

	a.HTTPServer.AddReadinessCheck("messaging", func(ctx context.Context) error {
		if a.Messaging == nil {
			return nil
		}
		if hc, ok := a.Messaging.(messaging.HealthChecker); ok {
			return hc.Healthy()
		}
		return nil
	})
}
