package host

import (
	"context"
	"fmt"

	"github.com/hatami57/microjet/httpx"
	"github.com/hatami57/microjet/messaging"
)

// WithHTTPServer creates the HTTP server and runs the optional setup handlers
// (typically route registration). It also registers default readiness probes
// (GET /readyz) for any registered databases and the messaging client. Errors
// are deferred to Run/MustRun/Err.
func (a *App) WithHTTPServer(setup ...HandlerFunc) *App {
	if a.err != nil {
		return a
	}
	a.HTTPServer = httpx.NewServer(httpx.ServerConfig{
		Host:  a.Config.Server.Host,
		Port:  a.Config.Server.Port,
		Debug: a.Config.App.Debug,
	}, a.Logger)

	a.registerDefaultReadinessChecks()

	for _, s := range setup {
		if s == nil {
			continue
		}
		if err := s(a); err != nil {
			return a.fail(err)
		}
	}
	return a
}

// registerDefaultReadinessChecks wires /readyz probes that read App state at
// request time, so databases or messaging registered before or after
// WithHTTPServer are both covered.
func (a *App) registerDefaultReadinessChecks() {
	a.HTTPServer.AddReadinessCheck("database", func(ctx context.Context) error {
		for name, db := range a.databases {
			sqlDB, err := db.DB()
			if err != nil {
				return fmt.Errorf("db %q: %w", name, err)
			}
			if err := sqlDB.PingContext(ctx); err != nil {
				return fmt.Errorf("db %q: %w", name, err)
			}
		}
		return nil
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
