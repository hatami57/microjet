// Command jetstream demonstrates the NATS JetStream driver
// (messaging/jetstream): durable, at-least-once delivery. Unlike core NATS,
// JetStream persists published messages to a stream and delivers them to a
// durable consumer with explicit acks — so a message is not lost if the
// consumer is momentarily down, and a handler that returns an error gets the
// message redelivered (up to maxDeliver, after which it is dead-lettered).
//
// Because jetstream.Client implements messaging.Client, it is a drop-in for the
// core NATS driver: the same messaging.Subscribe / messaging.Of(app).Publish
// APIs work unchanged, and installing it under an outbox upgrades that to
// at-least-once end to end.
//
// Needs a JetStream-enabled NATS server (config.toml points at
// nats://localhost:4222):
//
//	nats-server -js
//	go run .
//
// Watch the logs: every third order fails on its first delivery and is
// redelivered a couple of seconds later (ackWait), then succeeds — at-least-once
// in action.
package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/hatami57/microjet/host"
	"github.com/hatami57/microjet/messaging"
	"github.com/hatami57/microjet/messaging/jetstream"
)

const subject = "orders.created"

// OrderCreated is the typed event payload.
type OrderCreated struct {
	OrderID int     `json:"orderID"`
	Total   float64 `json:"total"`
}

func main() {
	// Install JetStream as the broker. The stream and its durable consumer come
	// from config.toml ([messaging.jetstream]); the host dials on start, and the
	// declared ORDERS stream is ensured (created or updated) on connect.
	app := host.MustNew().WithModule(messaging.Module(jetstream.New()))

	// failedOnce tracks order IDs we have already failed once, so the retry
	// succeeds. JetStream may deliver concurrently, so guard it.
	var mu sync.Mutex
	failedOnce := map[int]bool{}

	app.WithModule(messaging.Subscribe(subject, subject,
		messaging.HandleJSON(func(ctx context.Context, e OrderCreated) error {
			// Simulate a transient failure on the first delivery of every third
			// order to show redelivery. Returning an error naks the message; a nil
			// return acks it.
			if e.OrderID%3 == 0 {
				mu.Lock()
				first := !failedOnce[e.OrderID]
				failedOnce[e.OrderID] = true
				mu.Unlock()
				if first {
					app.Logger.Warn("transient failure, will be redelivered", "orderID", e.OrderID)
					return errors.New("simulated transient failure")
				}
			}
			app.Logger.Info("processed order", "orderID", e.OrderID, "total", e.Total)
			return nil
		}),
	))

	// Publish a durable event every couple of seconds. Publish blocks until the
	// server acknowledges the message is persisted to the stream.
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
