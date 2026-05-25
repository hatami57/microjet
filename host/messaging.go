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
