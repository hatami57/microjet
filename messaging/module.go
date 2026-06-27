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
	n := first(name)
	return host.KeyedModuleFunc(moduleKey("messaging.Client", n), func(app *host.App) error {
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

// Of returns the messaging client installed by Module under the optional name,
// panicking if none was installed.
func Of(app *host.App, name ...string) Client {
	return host.MustResolveService[Client](app, name...)
}

// Lookup returns the messaging client installed under the optional name and whether
// one was installed. Use it where absence is an expected, recoverable condition;
// prefer Of when the client must exist.
func Lookup(app *host.App, name ...string) (Client, bool) {
	return host.ResolveService[Client](app, name...)
}

// first returns the first name or "" — the default instance.
func first(name []string) string {
	if len(name) > 0 {
		return name[0]
	}
	return ""
}

// moduleKey builds a module dedup key for a slot and instance name.
func moduleKey(slot, name string) string {
	if name == "" {
		return slot
	}
	return slot + "[" + name + "]"
}
