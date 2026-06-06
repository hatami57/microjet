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

// New constructs an App, loading the standard configuration sections and the
// logger. Returns an error instead of panicking so callers can handle config
// failures gracefully. To load service-specific config sections call
// app.LoadConfig after construction.
func New(opts ...Option) (*App, error) {
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
	cfg := &Config{}
	if err := loader.Configure(cfg); err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	a.Config = cfg
	a.Logger = core.NewLogger(cfg.Log)
	return a, nil
}

// MustNew is like New but panics on error. Convenient for main().
func MustNew(opts ...Option) *App {
	a, err := New(opts...)
	if err != nil {
		panic(fmt.Errorf("host.MustNew: %w", err))
	}
	return a
}

// LoadConfig loads additional config sections using the app's shared config
// loader, so the config file is only parsed once. Call this right after New()
// to populate service-specific config structs before starting services.
func (a *App) LoadConfig(cfgs ...core.Configurable) *App {
	if a.err != nil {
		return a
	}
	if err := a.configLoader.Configure(cfgs...); err != nil {
		return a.fail(err)
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

	// Init() (called by initServices above) already started the listener goroutine.
	// ErrCh receives its outcome so Run can react to unexpected server failures.
	var httpErrCh <-chan error
	if a.HTTPServer != nil {
		httpErrCh = a.HTTPServer.ErrCh()
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

// StartHTTP blocks until the HTTP server stops and returns its exit error.
// The server must already be initialized (i.e. InitServices or Run must have
// been called). Kept for callers that manage the lifecycle manually.
func (a *App) StartHTTP() error {
	if a.HTTPServer == nil {
		return fmt.Errorf("http server not configured; call WithHTTPServer first")
	}
	return <-a.HTTPServer.ErrCh()
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
