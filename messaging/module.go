package messaging

import (
	"fmt"
	"log/slog"

	"github.com/hatami57/microjet/host"
)

// loggerAware is an optional interface for clients built before the App (and
// therefore before its logger) existed. Module calls SetLogger during
// registration so such a client logs through the configured logger.
type loggerAware interface {
	SetLogger(*slog.Logger)
}

// Module registers a Client as the app's broker. The host drives the full
// lifecycle: it loads the client's config (if it implements configx.Configurable),
// dials during init, and disconnects on shutdown. Pass any implementation — there
// is no built-in broker:
//
//	host.MustNew().WithModule(messaging.Module(nats.New())).MustRun()
//
// If the client implements SetLogger, it receives the host logger here. Reach the
// client from a Setup hook or handler with messaging.Of(app). Pass an optional
// name to register several brokers side by side, each retrieved with
// messaging.Of(app, name).
func Module(client Client, name ...string) host.Module {
	return host.ModuleFunc(func(app *host.App) error {
		if client == nil {
			return fmt.Errorf("messaging: nil client")
		}
		if la, ok := client.(loggerAware); ok {
			la.SetLogger(app.Logger)
		}
		host.ProvideService(app, client, name...)
		return nil
	})
}

// Of returns the messaging client installed by Module under the optional name, or
// nil if none was installed.
func Of(app *host.App, name ...string) Client {
	if c, ok := host.ResolveService[Client](app, name...); ok {
		return c
	}
	return nil
}
