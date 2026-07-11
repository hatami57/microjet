// Command outbox demonstrates the transactional outbox (outbox.Module): an event
// is recorded in the same database transaction as the domain write, and a
// periodic relay publishes pending events to the broker with at-least-once
// delivery. This closes the gap where a process could crash after committing a
// write but before publishing — the event is durable in the DB and will be
// relayed on recovery.
//
// The flow: POST /orders writes an Order row and enqueues an "orders.created"
// event in one transaction; the relay (running every 2s, and woken immediately by
// each enqueue) publishes it to NATS; the in-process subscriber logs it. The relay
// is configured to quarantine poison messages after 10 failed attempts and to
// prune rows published more than 24h ago.
//
// Needs a running NATS server; the database is local SQLite (created at runtime):
//
//	docker run --rm -p 4222:4222 nats:latest
//	go run .
//	curl -s -XPOST localhost:8080/orders -d '{"item":"book","qty":2}'
//
// Watch the logs: the relay publishes the pending event and the subscriber
// receives it a moment after the POST returns.
package main

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hatami57/microjet/gormx"
	"github.com/hatami57/microjet/gormx/sqlite"
	"github.com/hatami57/microjet/host"
	"github.com/hatami57/microjet/httpx"
	"github.com/hatami57/microjet/messaging"
	"github.com/hatami57/microjet/messaging/nats"
	"github.com/hatami57/microjet/outbox"
)

const subject = "orders.created"

type Order struct {
	ID   uint   `gorm:"primaryKey" json:"id"`
	Item string `gorm:"not null" json:"item"`
	Qty  int    `gorm:"not null" json:"qty"`
}

// OrderCreated is the event payload enqueued in the outbox and relayed to NATS.
type OrderCreated struct {
	OrderID uint   `json:"orderID"`
	Item    string `json:"item"`
	Qty     int    `json:"qty"`
}

func main() {
	app := host.MustNew().
		WithModule(gormx.Module(sqlite.Driver())).
		WithModule(messaging.Module(nats.New())).
		// The outbox module migrates its table on startup and runs the relay. The
		// relay drains every Interval (and promptly after each enqueue); MaxAttempts
		// quarantines a message that keeps failing to publish instead of retrying it
		// forever; Retention prunes long-published rows so the table stays bounded.
		WithModule(outbox.Module(
			outbox.Interval(2*time.Second),
			outbox.MaxAttempts(10),
			outbox.Retention(24*time.Hour),
		)).
		WithModule(httpx.Module())

	// Subscribe so we can see relayed events arrive (decoded with HandleJSON).
	app.WithModule(messaging.Subscribe(subject, subject,
		messaging.HandleJSON(func(ctx context.Context, e OrderCreated) error {
			app.Logger.Info("event received", "orderID", e.OrderID, "item", e.Item)
			return nil
		}),
	))

	app.Setup(func(a *host.App) error {
		return gormx.Of(a).AutoMigrate(&Order{})
	})

	app.Setup(func(a *host.App) error {
		db := gormx.Of(a)
		repo := gormx.NewBaseRepository(db)
		orders := gormx.NewTable[Order](db)
		enq := outbox.NewEnqueuer(db)
		httpx.Of(a).Router.POST("/orders", func(c *gin.Context) {
			body, err := httpx.Body[Order](c)
			if err != nil {
				c.Error(err)
				return
			}

			// The domain write and the event enqueue share one gormx transaction:
			// RunTx threads the tx through the context, both the Create and the
			// Enqueue join it, so either both commit or neither does — an event can
			// never be lost or orphaned.
			err = repo.RunTx(c.Request.Context(), func(ctx context.Context) error {
				if err := orders.Create(ctx, &body); err != nil {
					return err
				}
				return enq.EnqueueJSON(ctx, subject, OrderCreated{
					OrderID: body.ID,
					Item:    body.Item,
					Qty:     body.Qty,
				})
			})
			if err != nil {
				c.Error(err)
				return
			}
			c.JSON(http.StatusCreated, body)
		})
		return nil
	})

	app.MustRun()
}
