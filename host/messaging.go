package host

import (
	"github.com/hatami57/microjet/messaging"
)

// WithMessaging registers the NATS messaging client as a service. The host's
// initServices (called at the start of Run) loads config via NATSClient's
// Configurable implementation and then dials the broker via its Init method.
func (a *App) WithMessaging() *App {
	if a.err != nil {
		return a
	}
	client := messaging.NewNATSClient(a.Logger)
	a.Messaging = client
	ProvideService(a, client)
	return a
}

// WithMessagingClient injects a pre-built messaging.Client (e.g. an in-memory
// fake in tests, or a non-NATS implementation). Errors are deferred to
// Run/MustRun/Err.
func (a *App) WithMessagingClient(client messaging.Client) *App {
	if a.err != nil {
		return a
	}
	a.Messaging = client
	return a
}
