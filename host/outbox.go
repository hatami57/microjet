package host

import (
	"context"
	"fmt"
	"time"

	"github.com/hatami57/microjet/outbox"
)

// DefaultOutboxInterval is how often the relay drains the outbox when no
// interval is given to WithOutbox.
const DefaultOutboxInterval = 5 * time.Second

type outboxConfig struct {
	interval  time.Duration
	batchSize int
	dbName    string
}

// OutboxOption configures the outbox relay registered by WithOutbox.
type OutboxOption func(*outboxConfig)

// WithOutboxInterval sets how often the relay drains pending messages.
func WithOutboxInterval(d time.Duration) OutboxOption {
	return func(c *outboxConfig) {
		if d > 0 {
			c.interval = d
		}
	}
}

// WithOutboxBatchSize sets how many messages the relay drains per pass.
func WithOutboxBatchSize(n int) OutboxOption {
	return func(c *outboxConfig) {
		if n > 0 {
			c.batchSize = n
		}
	}
}

// WithOutboxDatabase selects which named database holds the outbox table
// (defaults to the primary database).
func WithOutboxDatabase(name string) OutboxOption {
	return func(c *outboxConfig) {
		if name != "" {
			c.dbName = name
		}
	}
}

// WithOutbox wires the transactional outbox into the app: it migrates the
// outbox table during setup (after the database connects, before serving) and
// runs a periodic relay that publishes pending messages to the configured
// broker. Requires WithDatabase (or WithNamedDatabase) and WithMessaging.
// Enqueue events with outbox.Enqueue / outbox.EnqueueJSON inside the same
// transaction as your domain write. Errors are deferred to Run/MustRun/Err.
func (a *App) WithOutbox(opts ...OutboxOption) *App {
	if a.err != nil {
		return a
	}
	cfg := outboxConfig{interval: DefaultOutboxInterval, batchSize: outbox.DefaultBatchSize, dbName: DefaultDatabase}
	for _, opt := range opts {
		opt(&cfg)
	}

	// Migrate the outbox table once resources are connected, before serving.
	a.Setup(func(app *App) error {
		db := app.NamedDB(cfg.dbName)
		if db == nil {
			return fmt.Errorf("outbox: no database %q; call WithDatabase first", cfg.dbName)
		}
		return outbox.Migrate(db)
	})

	a.WithPeriodicWorker("outbox-relay", cfg.interval, func(ctx context.Context, app *App) error {
		if app.Messaging == nil {
			return fmt.Errorf("outbox: no messaging client; call WithMessaging first")
		}
		db := app.NamedDB(cfg.dbName)
		if db == nil {
			return fmt.Errorf("outbox: no database %q", cfg.dbName)
		}
		relay := outbox.NewRelay(db, app.Messaging,
			outbox.WithBatchSize(cfg.batchSize), outbox.WithLogger(app.Logger))
		_, err := relay.PublishPending(ctx)
		return err
	})
	return a
}
