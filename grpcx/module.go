package grpcx

import (
	"context"
	"errors"

	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/host"
)

// Module installs a gRPC server into the App and manages its lifecycle: the host
// starts the listener during the start phase and drains it on close. Register
// generated services from a Setup hook (or app.Setup), which runs after services
// are initialized but before the server begins serving:
//
//	host.MustNew().
//	    WithModule(grpcx.Module()).
//	    Setup(func(a *host.App) error {
//	        pb.RegisterGreeterServer(grpcx.Of(a).Server(), &greeter{})
//	        return nil
//	    }).
//	    MustRun()
//
// The health service reports NOT_SERVING whenever any registered service
// implementing core.HealthChecker is unhealthy, so services added before or after
// this module are all covered.
//
// Pass an optional name to install several servers side by side; a named server
// reads its own [grpc.<name>] config section (so it can bind a different port)
// and is retrieved with grpcx.Of(app, name).
func Module(name ...string) host.Module {
	n := first(name)
	return host.KeyedModuleFunc(moduleKey("grpcx.Server", n), func(app *host.App) error {
		srv := NewServer(ServerConfig{Debug: app.Config.App.Debug}, app.Logger)
		if n != "" {
			srv.SetConfigSection("grpc." + n)
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

// CloseOrder closes the gRPC server early (as an edge), so it stops accepting
// RPCs and drains in-flight ones before the backends those handlers use
// (database, cache, broker) are torn down.
func (s *Server) CloseOrder() int { return host.CloseEdge }

// Of returns the gRPC server installed by Module under the optional name,
// panicking if none was installed. Use it from Setup hooks to register services,
// e.g. grpcx.Of(app).Server().
func Of(app *host.App, name ...string) *Server {
	return host.MustResolveService[*Server](app, name...)
}

// Lookup returns the gRPC server installed under the optional name and whether
// one was installed. Prefer Of when the server must exist.
func Lookup(app *host.App, name ...string) (*Server, bool) {
	return host.ResolveService[*Server](app, name...)
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
