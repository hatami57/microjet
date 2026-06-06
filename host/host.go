package host

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/hatami57/microjet/aws"
	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/httpx"
	"github.com/hatami57/microjet/messaging"
	"gorm.io/gorm"
)

// DefaultDatabase is the key used for the primary database registered via WithDatabase or WithPostgreSQL.
const DefaultDatabase = "default"

// App is the central runtime object for a service. Build it with the fluent
// New().With*() chain at service startup.
type App struct {
	Config     *Config
	Logger     *slog.Logger
	Clock      core.TimeProvider
	Messaging  messaging.Client
	AWS        *aws.AWS
	HTTPServer *httpx.Server

	envPrefix            string
	configLoader         *core.ConfigLoader
	shutdownTimeout      time.Duration
	databases            map[string]*gorm.DB
	container            sync.Map
	workers              []worker
	isServiceInitialized bool
	err                  error
	closeOnce            sync.Once
}

// HandlerFunc is a setup/lifecycle callback that receives the App and may fail.
type HandlerFunc func(app *App) error

// Option configures an App at construction time.
type Option func(*App)

// WithEnvPrefix overrides the environment-variable prefix used for config
// overrides (defaults to "APP", e.g. APP_SERVER_PORT).
func WithEnvPrefix(prefix string) Option {
	return func(a *App) { a.envPrefix = prefix }
}

// WithClock injects the time source used by the App and its components. Pass
// core.UTC in production (the default) or a *core.FixedClock in tests to make
// time-dependent behavior deterministic.
func WithClock(clock core.TimeProvider) Option {
	return func(a *App) { a.Clock = clock }
}

// WithShutdownTimeout bounds how long Close waits for managed resources (HTTP
// server, databases, messaging, services) to stop. Defaults to 15s.
func WithShutdownTimeout(d time.Duration) Option {
	return func(a *App) { a.shutdownTimeout = d }
}

// NewWithExtraConfig loads configuration (including a typed [extra] section) and returns
// a new App. Use this when your service has service-specific config beyond the standard fields.
// T is typically a struct matching your [extra] TOML section.
//
// Example:
//
//	type MyConfig struct { WorkerCount int `mapstructure:"workerCount"` }
//	app := host.NewWithExtraConfig[MyConfig]()
//	cfg := host.MustGetExtraConfig[MyConfig](app.Config)
func NewWithExtraConfig[T any](opts ...Option) (*App, error) {
	a := &App{}
	for _, opt := range opts {
		opt(a)
	}
	if a.Clock == nil {
		a.Clock = core.UTC
	}
	loader, err := core.NewConfigLoader(a.envPrefix)
	if err != nil {
		return nil, fmt.Errorf("creating config loader: %w", err)
	}
	a.configLoader = loader
	config, err := loadConfigWithExtra[T](loader)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	a.Config = config
	a.Logger = core.NewLogger(config.Log)
	return a, nil
}

// New constructs an App, loading configuration and the logger. Returns an
// error instead of panicking so callers can handle config failures gracefully.
// Use NewWithExtraConfig[T] if your service defines a typed [extra] config section.
func New(opts ...Option) (*App, error) {
	return NewWithExtraConfig[map[string]any](opts...)
}

// MustNewWithExtraConfig is like NewWithExtraConfig but panics on error. Convenient for main().
func MustNewWithExtraConfig[T any](opts ...Option) *App {
	a, err := NewWithExtraConfig[T](opts...)
	if err != nil {
		panic(fmt.Errorf("host.MustNew: %w", err))
	}
	return a
}

// MustNew is like New but panics on error. Convenient for main().
func MustNew(opts ...Option) *App {
	a, err := New(opts...)
	if err != nil {
		panic(fmt.Errorf("host.MustNew: %w", err))
	}
	return a
}

// Err returns the first error recorded while building the App via the fluent
// With*/Setup methods, or nil if the chain succeeded.
func (a *App) Err() error { return a.err }

// fail records the first build error and short-circuits the fluent chain.
func (a *App) fail(err error) *App {
	if a.err == nil && err != nil {
		a.err = err
	}
	return a
}

// DB returns the default database connection, or nil if none has been registered.
func (a *App) DB() *gorm.DB {
	return a.NamedDB(DefaultDatabase)
}

// NamedDB returns a named database connection, or nil if not found.
func (a *App) NamedDB(name string) *gorm.DB {
	if a.databases == nil {
		return nil
	}
	return a.databases[name]
}

// WithDatabase registers db as the default database.
// Called internally by WithPostgreSQL.
func (a *App) WithDatabase(db *gorm.DB) *App {
	return a.WithNamedDatabase(DefaultDatabase, db)
}

// WithNamedDatabase registers a named database for multi-database setups.
// Retrieve it later with NamedDB(name).
func (a *App) WithNamedDatabase(name string, db *gorm.DB) *App {
	if a.databases == nil {
		a.databases = make(map[string]*gorm.DB)
	}
	a.databases[name] = db
	return a
}

// WithDatabaseFromConfig connects the default database from the [database]
// config section, dispatching on its "driver" field. Supported drivers:
// "postgres"/"postgresql" and "sqlite"/"sqlite3" (case-insensitive). An empty or
// unknown driver defers an error to Run/MustRun/Err. For additional connections
// see WithDatabasesFromConfig.
func (a *App) WithDatabaseFromConfig() *App {
	if a.err != nil {
		return a
	}
	if a.Config.Database == nil {
		return a.fail(fmt.Errorf("database: no [database] config section"))
	}
	db, err := a.connectDatabase(a.Config.Database)
	if err != nil {
		return a.fail(fmt.Errorf("database: %w", err))
	}
	return a.WithDatabase(db)
}

// Setup runs a setup handler as part of the fluent chain (e.g. migrations or
// route registration). Errors are deferred and surfaced by Run/MustRun/Err.
func (a *App) Setup(handler HandlerFunc) *App {
	if a.err != nil || handler == nil {
		return a
	}
	if err := handler(a); err != nil {
		return a.fail(err)
	}
	return a
}

// Run initializes services, starts workers and the HTTP server (if configured),
// then blocks until a termination signal or fatal server error. On exit it
// cancels workers, waits for them, and gracefully shuts down.
func (a *App) Run() error {
	if a.err != nil {
		a.Close()
		return a.err
	}
	if err := a.initServices(); err != nil {
		a.Close()
		return fmt.Errorf("initializing services: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	workerWg := a.startWorkers(ctx)

	quit := notifySignals()

	var httpErrCh <-chan error
	if a.HTTPServer != nil {
		ch := make(chan error, 1)
		httpErrCh = ch
		go func() {
			a.Logger.Info("Starting HTTP server...", "addr", a.HTTPServer.Addr())
			// Always report the outcome: Start returns nil on a clean shutdown
			// (http.ErrServerClosed) and an error otherwise. Either way the server
			// is no longer serving, which is a reason to wind the app down.
			ch <- a.HTTPServer.Start()
		}()
	}

	var runErr error
	select {
	case sig := <-quit:
		a.Logger.Info("received shutdown signal", "signal", sig.String())
	case err := <-httpErrCh:
		if err != nil {
			a.Logger.Error("HTTP server failed", "error", err)
			runErr = err
		} else {
			a.Logger.Warn("HTTP server stopped unexpectedly")
		}
	}

	cancel()
	workerWg.Wait()
	a.Close()
	return runErr
}

// MustRun is like Run but logs and exits the process on error.
func (a *App) MustRun() {
	if err := a.Run(); err != nil {
		a.Logger.Error("application error", "error", err)
		os.Exit(1)
	}
}

// StartHTTP starts the HTTP server and blocks until it stops. Kept for
// callers that manage the lifecycle manually instead of via Run.
func (a *App) StartHTTP() error {
	if a.HTTPServer == nil {
		return fmt.Errorf("http server not configured; call WithHTTPServer first")
	}
	a.Logger.Info("Starting HTTP server...", "addr", a.HTTPServer.Addr())
	return a.HTTPServer.Start()
}

// WaitForExitSignal blocks until the process receives SIGINT or SIGTERM.
func WaitForExitSignal() {
	<-notifySignals()
}

// notifySignals returns a channel that receives SIGINT/SIGTERM. The buffer of 1
// ensures a signal arriving before the receiver is ready is not dropped.
func notifySignals() chan os.Signal {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	return quit
}

// Close gracefully shuts down all managed resources. Safe to call more than once.
func (a *App) Close() {
	a.closeOnce.Do(a.close)
}

func (a *App) close() {
	a.Logger.Info("Shutting down...")

	timeout := a.shutdownTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var wg sync.WaitGroup

	if a.HTTPServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := a.HTTPServer.Stop(ctx); err != nil {
				a.Logger.Error("Failed to stop http server", "error", err)
			}
		}()
	}

	for name, db := range a.databases {
		wg.Add(1)
		go func(name string, db *gorm.DB) {
			defer wg.Done()
			sqlDB, err := db.DB()
			if err != nil {
				a.Logger.Error("Failed to get db instance", "db", name, "error", err)
				return
			}
			if err := sqlDB.Close(); err != nil {
				a.Logger.Error("Failed to close db connection", "db", name, "error", err)
			}
		}(name, db)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		a.closeServices()
	}()

	// Bound the overall shutdown: a misbehaving service must not block exit
	// forever. Branches whose APIs don't take a context (Disconnect, db.Close)
	// are still covered by this deadline.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		a.Logger.Error("shutdown timed out; exiting anyway", "timeout", timeout)
	}
	a.Logger.Info("Goodbye!")
}
