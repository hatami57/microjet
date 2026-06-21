// Command messaging demonstrates MicroJet's NATS pub/sub (messaging + nats):
// messaging.Subscribe ties a subscription to the app lifecycle (subscribe on
// start, drain on shutdown), messaging.HandleJSON gives a typed handler with
// automatic decoding, and messaging.Of(app).Publish sends messages. A periodic
// worker publishes an event every couple of seconds so you can watch the
// subscriber receive it.
//
// Needs a running NATS server (config.toml points at nats://localhost:4222):
//
//	docker run --rm -p 4222:4222 nats:latest
//	go run .
//
// Watch the logs: every publish is followed by the subscriber logging the
// decoded payload.
package main

import (
	"context"
	"time"

	"github.com/hatami57/microjet/host"
	"github.com/hatami57/microjet/messaging"
	"github.com/hatami57/microjet/messaging/nats"
)

const subject = "orders.created"

// OrderCreated is the typed event payload. HandleJSON decodes incoming messages
// straight into it; NewJSONMessage encodes it for publishing.
type OrderCreated struct {
	OrderID int     `json:"orderID"`
	Total   float64 `json:"total"`
}

func main() {
	// Install NATS as the broker. The host dials it on start, drains on stop.
	app := host.MustNew().WithModule(messaging.Module(nats.New()))

	// Subscribe as a lifecycle-managed module: HandleJSON turns a
	// func(ctx, OrderCreated) error into a raw handler with decoding built in.
	// The closure captures app, so the handler can log through app.Logger.
	// WithQueueGroup(queue) would load-balance the subject across replicas.
	app.WithModule(messaging.Subscribe(subject, subject,
		messaging.HandleJSON(func(ctx context.Context, e OrderCreated) error {
			app.Logger.Info("received order", "orderID", e.OrderID, "total", e.Total)
			return nil
		}),
	))

	// Publish a new event on a fixed interval. The periodic worker stops when the
	// app shuts down.
	var seq int
	app.WithPeriodicWorker("publisher", 2*time.Second, func(ctx context.Context, a *host.App) error {
		seq++
		msg, err := messaging.NewJSONMessage(subject, OrderCreated{OrderID: seq, Total: float64(seq) * 9.99})
		if err != nil {
			return err
		}
		a.Logger.Info("publishing", "subject", subject, "orderID", seq)
		return messaging.Of(a).Publish(ctx, msg)
	})

	app.MustRun()
}
