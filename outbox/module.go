package outbox

import (
	"context"
	"log/slog"
	"time"

	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/gormx"
	"github.com/hatami57/microjet/host"
	"github.com/hatami57/microjet/messaging"
	"gorm.io/gorm"
)

// DefaultInterval is how often the relay drains the outbox when no interval is
// given to Module.
const DefaultInterval = 5 * time.Second

type config struct {
	interval    time.Duration
	batchSize   int
	maxAttempts int
	retention   time.Duration
	dbName      string
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

// MaxAttempts quarantines a message after it has failed to publish n times,
// stopping endless retries of a poison message. Zero (the default) retries
// forever.
func MaxAttempts(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.maxAttempts = n
		}
	}
}

// Retention deletes messages published longer than d ago at the end of each relay
// pass, keeping the table bounded. Zero (the default) keeps published rows
// forever.
func Retention(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.retention = d
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
// relay that publishes pending messages to the configured broker on each Interval
// (and promptly after an enqueue). Requires gormx.Module (optionally named) and
// messaging.Module. Resolve the shared enqueuer with outbox.Of(app) and enqueue
// events inside the same gormx transaction as your domain write.
//
// Multi-replica safety is dialect-dependent (see the Relay doc comment): on
// Postgres and MySQL the relay claims each batch with FOR UPDATE SKIP LOCKED, so
// running one replica per instance is safe. On SQLite (and any dialect without
// SKIP LOCKED) the relay is not replica-safe — run exactly one instance.
//
// Tune durability with MaxAttempts (quarantine poison messages instead of
// retrying them forever) and Retention (prune old published rows so the table
// stays bounded):
//
//	host.MustNew().WithModules(
//	    gormx.Module(postgres.Driver()),
//	    messaging.Module(nats.New()),
//	    outbox.Module(
//	        outbox.Interval(2*time.Second),
//	        outbox.MaxAttempts(10),
//	        outbox.Retention(24*time.Hour),
//	    ),
//	)
func Module(opts ...Option) host.Module {
	cfg := config{interval: DefaultInterval, batchSize: DefaultBatchSize, dbName: gormx.DefaultDatabase}
	for _, opt := range opts {
		opt(&cfg)
	}
	// Keyed by target database so two relays for the same DB deduplicate (avoiding
	// duplicate migrations and double-publishing) while relays for distinct named
	// databases coexist.
	return host.KeyedModuleFunc("outbox.Relay["+cfg.dbName+"]", func(app *host.App) error {
		// A successful enqueue nudges the relay through this channel so it drains
		// promptly instead of waiting out the interval. Buffered to depth 1 so the
		// non-blocking send never stalls the enqueuing transaction and repeated
		// enqueues coalesce into a single pending wake-up.
		notify := make(chan struct{}, 1)

		// Register a stable enqueuer now so callers can resolve outbox.Of(app)
		// during their own Init/Setup; its table is wired by relayService.Setup
		// once the database is connected (mirrors how gormx.Of fills in the
		// connection during init).
		enq := &Enqueuer{notify: notify}
		host.ProvideService(app, enq, cfg.dbName)

		// Register the relay as a service so the host calls its Setup hook once
		// resources are connected (migrating the table, wiring the enqueuer, and
		// building the single long-lived relay the worker reuses each pass).
		svc := &relayService{cfg: cfg, enq: enq, notify: notify}
		host.ProvideService(app, svc, cfg.dbName)

		app.WithWorker("outbox-relay", func(ctx context.Context, a *host.App) error {
			return svc.run(ctx)
		})
		return nil
	})
}

// Of returns the shared Enqueuer for the outbox in the named database (default
// database when unnamed), panicking if outbox.Module is not installed for it.
func Of(app *host.App, name ...string) *Enqueuer {
	return host.MustResolveService[*Enqueuer](app, resolveDBName(name))
}

// Lookup returns the shared Enqueuer for the named database and whether one is
// registered. Prefer Of when the outbox must exist.
func Lookup(app *host.App, name ...string) (*Enqueuer, bool) {
	return host.ResolveService[*Enqueuer](app, resolveDBName(name))
}

// resolveDBName maps an optional database name to the key Module registered the
// Enqueuer under, defaulting to the primary database.
func resolveDBName(name []string) string {
	if len(name) > 0 && name[0] != "" {
		return name[0]
	}
	return gormx.DefaultDatabase
}

// relayService owns the outbox's setup and its single long-lived relay. It is
// registered as a service so the host invokes Setup once resources are connected;
// the worker then reuses the built relay each pass rather than rebuilding it (and
// re-resolving the database and broker) on every tick.
type relayService struct {
	cfg    config
	enq    *Enqueuer
	relay  *Relay
	notify <-chan struct{}
	logger *slog.Logger
}

// Setup is the ServiceSetupper hook the host calls after gormx and messaging have
// connected, before serving. It migrates the outbox table, wires the shared
// enqueuer to the database, and builds the relay, failing clearly if the required
// database or messaging client is not installed.
func (s *relayService) Setup(app *host.App) error {
	db, ok := gormx.Lookup(app, s.cfg.dbName)
	if !ok {
		return errorx.NewInternalError("outbox", "no database; install gormx.Module first", "database", s.cfg.dbName)
	}
	client, ok := messaging.Lookup(app)
	if !ok {
		return errorx.NewInternalError("outbox", "no messaging client; install messaging.Module first")
	}
	return s.wire(app, db, client)
}

// wire migrates the table, connects the enqueuer, and builds the relay. Split from
// Setup so it can be exercised directly with a plain publisher, without a
// container-registered messaging client.
func (s *relayService) wire(app *host.App, db *gorm.DB, pub messaging.Publisher) error {
	if err := Migrate(db); err != nil {
		return err
	}
	s.enq.table = gormx.NewTable[Message](db)
	s.logger = app.Logger
	s.relay = NewRelay(db, pub,
		WithBatchSize(s.cfg.batchSize),
		WithMaxAttempts(s.cfg.maxAttempts),
		WithRetention(s.cfg.retention),
		WithLogger(app.Logger),
		WithClock(app.Clock),
	)
	return nil
}

// run drives the relay: it drains (and prunes) immediately, then again on every
// interval tick or enqueue signal, until ctx is cancelled. Per-pass errors are
// logged, never fatal, so the relay keeps running.
func (s *relayService) run(ctx context.Context) error {
	ticker := time.NewTicker(s.cfg.interval)
	defer ticker.Stop()
	for {
		s.runPass(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case <-s.notify:
		}
	}
}

// runPass drains one batch to the broker and prunes expired published rows,
// logging (but not propagating) failures so a bad pass never stops the worker.
func (s *relayService) runPass(ctx context.Context) {
	if _, err := s.relay.PublishPending(ctx); err != nil && ctx.Err() == nil {
		s.logger.Error("outbox: relay pass failed", "error", err)
	}
	if n, err := s.relay.PrunePublished(ctx); err != nil {
		if ctx.Err() == nil {
			s.logger.Error("outbox: pruning failed", "error", err)
		}
	} else if n > 0 {
		s.logger.Debug("outbox: pruned published messages", "count", n)
	}
}
