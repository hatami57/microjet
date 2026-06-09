package host

import (
	"fmt"
	"log/slog"

	"github.com/hatami57/microjet/messaging"
)

// loggerAware is an optional interface for injected services that were built
// before the App (and therefore before its logger) existed. The host calls
// SetLogger during registration so such a service logs through the configured
// logger. Implementing it is optional; services that take a logger at
// construction can ignore it.
type loggerAware interface {
	SetLogger(*slog.Logger)
}

// WithMessaging registers a messaging.Client as the app's broker. The host
// drives the full lifecycle: it loads the client's config (if it implements
// core.Configurable), dials during init, and disconnects on shutdown. Pass any
// implementation — the host has no built-in broker:
//
//	app.WithMessaging(nats.New())
//
// If the client implements SetLogger, it receives the host logger here so a
// client constructed inline in the fluent chain still logs correctly.
func (a *App) WithMessaging(client messaging.Client) *App {
	if a.err != nil {
		return a
	}
	if client == nil {
		return a.fail(fmt.Errorf("messaging: nil client"))
	}
	if la, ok := client.(loggerAware); ok {
		la.SetLogger(a.Logger)
	}
	a.Messaging = client
	ProvideService(a, client)
	return a
}
