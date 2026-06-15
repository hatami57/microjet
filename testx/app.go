// Package testx provides helpers for testing services built with microjet: a
// host.App pre-wired with deterministic, in-memory defaults; an in-memory
// messaging broker; and small HTTP request/response helpers around a gin router.
// It exists so application tests don't re-derive the same scaffolding (a fixed
// clock, an in-memory database and cache, a fake broker) in every package.
package testx

import (
	"log/slog"
	"testing"
	"time"

	"github.com/hatami57/microjet/cache"
	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/gormx"
	"github.com/hatami57/microjet/gormx/sqlite"
	"github.com/hatami57/microjet/host"
	"gorm.io/gorm"
)

// DefaultFixedTime is the instant a test app's clock is pinned to unless
// overridden, so time-dependent behavior is reproducible.
var DefaultFixedTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type appConfig struct {
	clock     core.TimeProvider
	withDB    bool
	withCache bool
	withHTTP  bool
	broker    *Broker
	setups    []func(*host.App) error
}

// AppOption configures a test App built by NewApp.
type AppOption func(*appConfig)

// WithClock overrides the time provider (default: a FixedClock at
// DefaultFixedTime).
func WithClock(clock core.TimeProvider) AppOption {
	return func(c *appConfig) {
		if clock != nil {
			c.clock = clock
		}
	}
}

// WithoutDatabase skips registering the in-memory SQLite database.
func WithoutDatabase() AppOption {
	return func(c *appConfig) { c.withDB = false }
}

// WithoutCache skips registering the in-memory cache.
func WithoutCache() AppOption {
	return func(c *appConfig) { c.withCache = false }
}

// WithBroker registers an in-memory messaging broker as the app's client, so
// publish/subscribe code can be exercised without a real broker. The same
// *Broker is returned by App for assertions.
func WithBroker(b *Broker) AppOption {
	return func(c *appConfig) { c.broker = b }
}

// WithHTTPServer registers an HTTP server so app.HTTPServer (and its Router) are
// available after NewApp returns. The server is initialized but never starts
// listening — drive it in tests through testx.Request(app.HTTPServer.Router,
// ...). Register routes in a WithSetup hook, which runs once the router exists.
func WithHTTPServer() AppOption {
	return func(c *appConfig) { c.withHTTP = true }
}

// WithSetup registers a hook run after services are initialized (so app.DB(),
// app.Cache(), app.HTTPServer, and DI are ready) — use it to provide services or
// register HTTP routes. A hook returning an error fails the test.
func WithSetup(fn func(*host.App) error) AppOption {
	return func(c *appConfig) { c.setups = append(c.setups, fn) }
}

// App is a host.App built for tests plus the in-memory dependencies wired into
// it, exposed for assertions.
type App struct {
	*host.App
	Broker *Broker
}

// NewApp builds a host.App with deterministic in-memory defaults: a FixedClock,
// an in-memory SQLite database, and an in-memory cache. It initializes services
// (so app.DB() and app.Cache() are ready), runs any WithSetup hooks, and
// registers cleanup with t. Use options to trim or extend the wiring.
func NewApp(t testing.TB, opts ...AppOption) *App {
	t.Helper()
	cfg := appConfig{clock: core.NewFixedClock(DefaultFixedTime), withDB: true, withCache: true}
	for _, opt := range opts {
		opt(&cfg)
	}

	app, err := host.New(host.WithClock(cfg.clock))
	if err != nil {
		t.Fatalf("testx: host.New: %v", err)
	}
	if cfg.withDB {
		app.InjectDatabase(NewDB(t))
	}
	if cfg.withCache {
		app.WithCacheClient(cache.NewMemoryCache(cfg.clock))
	}
	if cfg.broker != nil {
		app.WithMessaging(cfg.broker)
	}
	if cfg.withHTTP {
		app.WithHTTPServer()
	}
	app.InitServices()
	if err := app.Err(); err != nil {
		t.Fatalf("testx: initializing services: %v", err)
	}
	for _, fn := range cfg.setups {
		if err := fn(app); err != nil {
			t.Fatalf("testx: setup hook: %v", err)
		}
	}
	t.Cleanup(app.Close)
	return &App{App: app, Broker: cfg.broker}
}

// NewDB opens a fresh in-memory SQLite database via the gormx SQLite driver and
// registers cleanup with t. Handy for repository tests that need a real
// *gorm.DB without the full app.
func NewDB(t testing.TB) *gorm.DB {
	t.Helper()
	db, err := sqlite.Driver().Open(gormx.Config{Name: ":memory:"}, slog.New(slog.NewTextHandler(discard{}, nil)))
	if err != nil {
		t.Fatalf("testx: open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// discard is an io.Writer that drops everything, used to silence the test DB's
// logger without importing io just for io.Discard semantics elsewhere.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
