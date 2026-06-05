package host

import (
	"fmt"

	"github.com/hatami57/microjet/messaging"
)

// WithMessaging connects to the messaging broker (NATS) using the [messaging]
// config section and runs the optional setup handlers (typically subscriptions).
// Errors are deferred to Run/MustRun/Err.
func (a *App) WithMessaging(setup ...HandlerFunc) *App {
	if a.err != nil {
		return a
	}
	client, err := messaging.New(a.Config.Messaging, a.Logger)
	if err != nil {
		return a.fail(fmt.Errorf("messaging: %w", err))
	}
	a.Messaging = client
	return a.runMessagingSetup(setup...)
}

// WithMessagingClient injects a pre-built messaging.Client (e.g. an in-memory
// fake in tests, or a non-NATS implementation) instead of dialing NATS, then
// runs the optional setup handlers. Errors are deferred to Run/MustRun/Err.
func (a *App) WithMessagingClient(client messaging.Client, setup ...HandlerFunc) *App {
	if a.err != nil {
		return a
	}
	a.Messaging = client
	return a.runMessagingSetup(setup...)
}

func (a *App) runMessagingSetup(setup ...HandlerFunc) *App {
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
