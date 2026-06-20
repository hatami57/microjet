package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/hatami57/microjet/gormx"
	"github.com/hatami57/microjet/host"
	"github.com/hatami57/microjet/messaging"
)

// DefaultInterval is how often the relay drains the outbox when no interval is
// given to Module.
const DefaultInterval = 5 * time.Second

type config struct {
	interval  time.Duration
	batchSize int
	dbName    string
}

// Option configures the outbox relay installed by Module.
type Option func(*config)

// Interval sets how often the relay drains pending messages.
func Interval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.interval = d
		}
	}
}

// BatchSize sets how many messages the relay drains per pass.
func BatchSize(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.batchSize = n
		}
	}
}

// Database selects which named database holds the outbox table (defaults to the
// primary database).
func Database(name string) Option {
	return func(c *config) {
		if name != "" {
			c.dbName = name
		}
	}
}

// Module wires the transactional outbox into the app: it migrates the outbox
// table during setup (after the database connects, before serving) and runs a
// periodic relay that publishes pending messages to the configured broker.
// Requires gormx.Module (optionally named) and messaging.Module. Enqueue
// events with outbox.Enqueue / outbox.EnqueueJSON inside the same transaction as
// your domain write.
//
//	host.MustNew().WithModules(
//	    gormx.Module(postgres.Driver()),
//	    messaging.Module(nats.New()),
//	    outbox.Module(outbox.Interval(2*time.Second)),
//	)
func Module(opts ...Option) host.Module {
	return host.ModuleFunc(func(app *host.App) error {
		cfg := config{interval: DefaultInterval, batchSize: DefaultBatchSize, dbName: gormx.DefaultDatabase}
		for _, opt := range opts {
			opt(&cfg)
		}

		// Migrate the outbox table once resources are connected, before serving.
		app.Setup(func(a *host.App) error { return migrateTable(a, cfg.dbName) })

		app.WithPeriodicWorker("outbox-relay", cfg.interval, func(ctx context.Context, a *host.App) error {
			return relayOnce(ctx, a, cfg)
		})
		return nil
	})
}

// migrateTable creates the outbox table in the named database, failing clearly
// if no such database is installed. Run during the host's setup phase.
func migrateTable(app *host.App, dbName string) error {
	db := gormx.Of(app, dbName)
	if db == nil {
		return fmt.Errorf("outbox: no database %q; install gormx.Module first", dbName)
	}
	return Migrate(db)
}

// relayOnce drains a single batch of pending messages to the broker. It is the
// body of the periodic relay worker, factored out so its wiring (and the missing
// database / messaging guards) can be exercised directly.
func relayOnce(ctx context.Context, app *host.App, cfg config) error {
	client := messaging.Of(app)
	if client == nil {
		return fmt.Errorf("outbox: no messaging client; install messaging.Module first")
	}
	db := gormx.Of(app, cfg.dbName)
	if db == nil {
		return fmt.Errorf("outbox: no database %q", cfg.dbName)
	}
	relay := NewRelay(db, client, WithBatchSize(cfg.batchSize), WithLogger(app.Logger))
	_, err := relay.PublishPending(ctx)
	return err
}
