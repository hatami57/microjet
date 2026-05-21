package host

import (
	"os"

	"github.com/hatami57/microjet/messaging"
)

func (a *App) WithMessaging(setup HandlerFunc) *App {
	client, err := messaging.New(a.Config.Messaging, a.Logger)
	if err != nil {
		a.Logger.Error("failed to connect to messaging", "error", err)
		os.Exit(1)
	}
	a.Messaging = client

	if setup != nil {
		a.MustRun(setup)
	}

	return a
}
