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

	envPrefix       string
	configReader    configx.Reader
	configValues    map[string]any
	shutdownTimeout time.Duration
	container       sync.Map
	// keys logs container keys in registration order so the lifecycle phases
	// (init, setup, start, close) iterate deterministically rather than in the
	// sync.Map's random Range order. keysMu guards appends to the slice; see
	// storeKey and orderedRange.
	keys                 []any
	keysMu               sync.Mutex
	modules              sync.Map
	workers              []worker
	setups               []HandlerFunc
	isServiceInitialized bool
	isServiceSetup       bool
	isServiceStarted     bool
	err                  error
	closeOnce            sync.Once

	// Runtime state for the explicit Start/Wait/Shutdown API. Populated by Start
	// and consumed by Wait and Shutdown; Run drives the same fields internally.
	workerCtx    context.Context
	workerCancel context.CancelFunc
	workerWg     *sync.WaitGroup
	fatalCh      <-chan error
	shutdownCh   chan struct{} // closed by Shutdown to unblock Wait
	startOnce    sync.Once
	startErr     error
	waitOnce     sync.Once
	waitErr      error
	shutdownOnce sync.Once
	shutdownErr  error
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

// WithCloseTimeout bounds how long Close waits for managed resources (HTTP
// server, databases, messaging, services) to stop. Defaults to 15s.
func WithCloseTimeout(d time.Duration) Option {
	return func(a *App) { a.shutdownTimeout = d }
}

// WithConfigReader injects the configuration source, bypassing the default TOML
// file discovery. Use it to embed the App in a host process that already owns
// configuration, or to supply fixed config in tests (see configx.NewMapReader).
//
// Precedence note: whatever the injected Reader returns is authoritative. The
// default env-var override shim applies only to the built-in Viper file reader,
// so an injected reader is not subject to it unless it implements that itself.
func WithConfigReader(r configx.Reader) Option {
	return func(a *App) { a.configReader = r }
}

// WithConfigValue sets a single configuration value in code, keyed by its dotted
// path (e.g. "app.debug", "app.shutdownDelay", "http.port"). Programmatic values
// are authoritative: they win over config files, environment variables, and
// defaults. Apply several at once with WithConfigValues.
//
// It works with the default file reader and with any injected reader that
// implements configx.Setter (both the file reader and configx.NewMapReader do);
// New returns an error if the active reader does not support it.
func WithConfigValue(key string, value any) Option {
	return func(a *App) {
		if a.configValues == nil {
			a.configValues = make(map[string]any)
		}
		a.configValues[key] = value
	}
}

// WithConfigValues sets several configuration values in code at once, each keyed
// by its dotted path. Equivalent to calling WithConfigValue per entry; when keys
// collide the last write wins. See WithConfigValue for precedence and reader
// requirements.
func WithConfigValues(values map[string]any) Option {
	return func(a *App) {
		if a.configValues == nil {
			a.configValues = make(map[string]any, len(values))
		}
		for k, v := range values {
			a.configValues[k] = v
		}
	}
}

// New constructs an App, loading the standard configuration sections and the
// logger. Returns an error instead of panicking so callers can handle config
// failures gracefully. To load service-specific config sections call
// app.Configure after construction.
func New(opts ...Option) (*App, error) {
	a := &App{shutdownCh: make(chan struct{})}
	for _, opt := range opts {
		opt(a)
	}
	if a.Clock == nil {
		a.Clock = core.UTC
	}
	// Only build the default file reader when one was not injected via
	// WithConfigReader — the seam Configure and per-service ReadConfig share.
	if a.configReader == nil {
		reader, err := configx.NewViperConfigReader(a.envPrefix)
		if err != nil {
			return nil, fmt.Errorf("creating config loader: %w", err)
		}
		a.configReader = reader
	}
	// Layer any programmatic values (WithConfigValue/WithConfigValues) on top of
	// the reader before anything reads from it, so they win over files and env.
	if len(a.configValues) > 0 {
		setter, ok := a.configReader.(configx.Setter)
		if !ok {
			return nil, fmt.Errorf("config reader %T does not support programmatic values set via WithConfigValue/WithConfigValues", a.configReader)
		}
		for key, value := range a.configValues {
			setter.Set(key, value)
		}
	}
	cfg := &Config{}
	if err := cfg.ReadConfig(a.configReader); err != nil {
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
// Within the chain they run in registration order.
// If services are already initialized (the manual InitServices path) the handler runs immediately.
// Errors are deferred and surfaced by Run/MustRun/Err.
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
	if err := a.Start(context.Background()); err != nil {
		return err
	}

	// Run owns signal handling: a SIGINT/SIGTERM begins graceful shutdown just
	// like a fatal service error does. Wait blocks until either happens; running
	// it in a goroutine lets the select race it against the signal channel.
	quit := notifySignals()
	waitDone := make(chan error, 1)
	go func() { waitDone <- a.Wait() }()

	var runErr error
	select {
	case sig := <-quit:
		a.Logger.Info("received shutdown signal", "signal", sig.String())
	case runErr = <-waitDone:
	}

	// Background ctx preserves Run's historical behavior: drain for the full
	// ShutdownDelay and wait for workers indefinitely, with Close bounded only by
	// WithCloseTimeout. Shutdown's own ctx-deadline bounding is for embedded
	// callers that supply one.
	_ = a.Shutdown(context.Background())
	return runErr
}

// beginShutdown flips every core.ReadinessToggler service to not-ready and then
// waits app.shutdownDelay before the host cancels workers and closes services.
// On Kubernetes this gives kube-proxy/endpoints time to drop the pod so new
// requests stop arriving (readiness starts failing) while in-flight ones drain;
// liveness (/health) stays healthy so the kubelet does not restart the pod
// mid-drain. With the default shutdownDelay of 0 it flips readiness and returns
// immediately, preserving the previous behavior. Cancelling ctx cuts the drain
// short, so a Shutdown deadline can bound it.
func (a *App) beginShutdown(ctx context.Context) {
	flipped := 0
	a.RangeServices(func(v any) bool {
		if t, ok := v.(core.ReadinessToggler); ok {
			t.SetReady(false)
			flipped++
		}
		return true
	})

	delay := a.Config.App.ShutdownDelay
	if flipped == 0 || delay <= 0 {
		return
	}
	a.Logger.Info("readiness flipped to not-ready; draining before shutdown", "delay", delay)
	select {
	case <-time.After(delay):
	case <-ctx.Done():
	}
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
		// Each failure is already logged per-service inside closeServices, and
		// Close has no error to return, so the joined error is discarded here.
		_ = a.closeServices()
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
