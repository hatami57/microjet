// Package host is MicroJet's application orchestrator: a fluent builder with a
// dependency-injection container, composable modules, background workers, and a
// managed lifecycle with graceful shutdown.
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

	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/core/configx"
	"github.com/hatami57/microjet/core/logx"
)

// App is the central runtime object for a service. Build it with the fluent
// New().With*() chain at service startup.
type App struct {
	Config *Config
	Logger *slog.Logger
	Clock  core.TimeProvider

	envPrefix            string
	configReader         configx.Reader
	shutdownTimeout      time.Duration
	container            sync.Map
	modules              sync.Map
	workers              []worker
	setups               []HandlerFunc
	isServiceInitialized bool
	isServiceSetup       bool
	isServiceStarted     bool
	err                  error
	closeOnce            sync.Once
}

// HandlerFunc is a setup/lifecycle callback that receives the App and may fail.
type HandlerFunc func(app *App) error

// Option configures an App at construction time.
type Option func(*App)

// WithEnvPrefix overrides the environment-variable prefix used for config
// overrides (defaults to "APP", e.g. APP_HTTP_PORT).
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
	reader, err := configx.NewViperConfigReader(a.envPrefix)
	if err != nil {
		return nil, fmt.Errorf("creating config loader: %w", err)
	}
	a.configReader = reader
	cfg := &Config{}
	if err := cfg.ReadConfig(reader); err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	a.Config = cfg
	a.Logger = logx.NewLogger(cfg.Log, cfg.App.Debug)
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

// Configure calls ReadConfig on each Configurable using the app's shared config
// reader, so the config file is only parsed once. Call this right after New()
// to populate service-specific config structs before starting services.
func (a *App) Configure(cfgs ...configx.Configurable) *App {
	if a.err != nil {
		return a
	}
	for _, cfg := range cfgs {
		if err := cfg.ReadConfig(a.configReader); err != nil {
			return a.fail(err)
		}
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

// Setup queues a setup handler (e.g. migrations or route registration). Handlers
// run after services are initialized — so connected resources (databases,
// caches, brokers) are available — but before the HTTP server starts serving.
// Within the chain
// they run in registration order. If services are already initialized (the
// manual InitServices path) the handler runs immediately. Errors are deferred
// and surfaced by Run/MustRun/Err.
func (a *App) Setup(handler ...HandlerFunc) *App {
	if a.err != nil || handler == nil {
		return a
	}
	a.setups = append(a.setups, handler...)
	if a.isServiceInitialized {
		if err := a.runSetups(); err != nil {
			return a.fail(err)
		}
	}
	return a
}

// runSetups drains and runs queued setup handlers in order. Draining (rather than
// ranging) lets a setup handler enqueue further setups. Safe to call repeatedly;
// a no-op when the queue is empty.
func (a *App) runSetups() error {
	for len(a.setups) > 0 {
		handler := a.setups[0]
		a.setups = a.setups[1:]
		if err := handler(a); err != nil {
			return err
		}
	}
	return nil
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
	// Resources are connected; run setup (migrations, route registration) before
	// anything begins serving — first the services' own Setup hooks, then the
	// queued app.Setup handlers, which observe the services' setup.
	if err := a.setupServices(); err != nil {
		a.Close()
		return fmt.Errorf("setting up services: %w", err)
	}
	if err := a.runSetups(); err != nil {
		a.Close()
		return fmt.Errorf("running setup: %w", err)
	}
	if err := a.startServices(); err != nil {
		a.Close()
		return fmt.Errorf("starting services: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	workerWg := a.startWorkers(ctx)

	quit := notifySignals()

	// startServices started any background listeners (e.g. the HTTP server's
	// goroutine); their ErrSource channels are merged so Run reacts to any such
	// service exiting the same way it reacts to a shutdown signal.
	fatalCh := a.fatalErrCh()

	var runErr error
	select {
	case sig := <-quit:
		a.Logger.Info("received shutdown signal", "signal", sig.String())
	case err := <-fatalCh:
		if err != nil {
			a.Logger.Error("service exited with error", "error", err)
			runErr = err
		} else {
			a.Logger.Warn("service stopped unexpectedly")
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

// fatalErrCh fans every started service's ErrSource channel into one. A receive
// on the returned channel means some service's background loop exited; the value
// is its error (or nil for a clean but unexpected stop). It returns nil — which
// blocks forever in a select — when no registered service exposes a channel.
func (a *App) fatalErrCh() <-chan error {
	var chans []<-chan error
	a.RangeServices(func(v any) bool {
		if src, ok := v.(ErrSource); ok {
			chans = append(chans, src.ErrCh())
		}
		return true
	})
	if len(chans) == 0 {
		return nil
	}
	out := make(chan error, len(chans))
	for _, ch := range chans {
		go func(c <-chan error) { out <- <-c }(ch)
	}
	return out
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

	wg.Go(func() {
		a.closeServices()
	})

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
