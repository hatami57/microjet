package httpx

import (
	"context"
	"errors"

	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/host"
)

// Module installs an HTTP server into the App and manages its lifecycle: the
// host starts the listener during the start phase and shuts it down on close.
// Register routes from a feature module's Setup hook (or app.Setup), which runs
// after services are initialized but before the server begins serving:
//
//	host.MustNew().
//	    WithModule(httpx.Module()).
//	    Setup(func(a *host.App) error {
//	        httpx.Of(a).Router.GET("/healthz", handler)
//	        return nil
//	    }).
//	    MustRun()
//
// The server registers a /readyz probe that, at request time, asks every
// registered service implementing core.HealthChecker whether it is ready, so
// services added before or after this module are all covered.
//
// Pass an optional name to install several servers side by side; a named server
// reads its own [http.<name>] config section (so it can bind a different port)
// and is retrieved with httpx.Of(app, name).
func Module(name ...string) host.Module {
	n := first(name)
	return host.KeyedModuleFunc(moduleKey("httpx.Server", n), func(app *host.App) error {
		srv := NewServer(ServerConfig{Debug: app.Config.App.Debug}, app.Logger)
		if n != "" {
			srv.SetConfigSection("http." + n)
		}

		srv.AddReadinessCheck("services", func(ctx context.Context) error {
			var errs error
			app.RangeServices(func(svc any) bool {
				if hc, ok := svc.(core.HealthChecker); ok {
					if err := hc.Healthy(ctx); err != nil {
						errs = errors.Join(errs, err)
					}
				}
				return true
			})
			return errs
		})

		host.ProvideService(app, srv, name...)
		return nil
	})
}

// CloseOrder closes the HTTP server early (as an edge), so it stops accepting
// connections and drains in-flight requests before the backends those handlers
// use (database, cache, broker) are torn down.
func (s *Server) CloseOrder() int { return host.CloseEdge }

// Of returns the HTTP server installed by Module under the optional name,
// panicking if none was installed. Use it from Setup hooks and handlers to reach
// the router, e.g. httpx.Of(app).Router or httpx.Of(app, "admin").Router.
func Of(app *host.App, name ...string) *Server {
	return host.MustResolveService[*Server](app, name...)
}

// first returns the first name or "" — the default instance.
func first(name []string) string {
	if len(name) > 0 {
		return name[0]
	}
	return ""
}

// moduleKey builds a module dedup key for a slot and instance name, so two
// installs of the same named slot deduplicate while distinct names coexist.
func moduleKey(slot, name string) string {
	if name == "" {
		return slot
	}
	return slot + "[" + name + "]"
}
