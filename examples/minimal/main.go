// Command minimal is the smallest possible MicroJet service: it loads config,
// sets up the logger, and waits for a termination signal.
package main

import "github.com/hatami57/microjet/host"

func main() {
	app := host.MustNew()
	defer app.Close()

	app.Logger.Info("minimal MicroJet service started")
	host.WaitForExitSignal()
}
