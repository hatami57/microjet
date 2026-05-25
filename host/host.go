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
	libhttp "github.com/hatami57/microjet/http"
	"github.com/hatami57/microjet/messaging"
	"gorm.io/gorm"
)

type App struct {
	Config     *core.Config
	Logger     *slog.Logger
	Messaging  messaging.Client
	AWS        *aws.AWS
	HTTPServer *libhttp.Server
	DB         *gorm.DB

	envPrefix            string
	container            sync.Map
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

// New constructs an App, loading configuration and the logger. It returns an
// error instead of panicking so it can be used safely in libraries and tests.
func New(opts ...Option) (*App, error) {
	a := &App{}
	for _, opt := range opts {
		opt(a)
	}

	config := &core.Config{}
	if err := core.Load(config, a.envPrefix); err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	a.Config = config
	a.Logger = core.NewLogger(config.Log)
	return a, nil
}

// MustNew is like New but panics on error. Convenient for main().
func MustNew(opts ...Option) *App {
	a, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return a
}

// Err returns the first error recorded while building the App via the fluent
// With* / Setup methods, or nil if the chain succeeded.
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

// Run initializes services, starts the HTTP server (if configured), blocks
// until a termination signal or a fatal server error, then gracefully shuts
// down. It returns any startup or server error.
func (a *App) Run() error {
	if a.err != nil {
		a.Close()
		return a.err
	}
	if err := a.initServices(); err != nil {
		a.Close()
		return fmt.Errorf("initializing services: %w", err)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	var httpErr chan error
	if a.HTTPServer != nil {
		httpErr = make(chan error, 1)
		go func() {
			if err := a.StartHTTP(); err != nil {
				httpErr <- err
			}
		}()
	}

	select {
	case sig := <-quit:
		a.Logger.Info("received shutdown signal", "signal", sig.String())
		a.Close()
		return nil
	case err := <-httpErr:
		a.Logger.Error("HTTP server failed", "error", err)
		a.Close()
		return err
	}
}

// MustRun is like Run but logs and exits the process on error.
func (a *App) MustRun() {
	if err := a.Run(); err != nil {
		a.Logger.Error("application error", "error", err)
		a.Close()
		os.Exit(1)
	}
}

// WaitForExitSignal blocks until the process receives SIGINT or SIGTERM. Use
// it when managing the lifecycle manually instead of via Run.
func WaitForExitSignal() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
}

// Close gracefully shuts down all managed resources. It is safe to call more
// than once (e.g. via both Run and a deferred Close).
func (a *App) Close() {
	a.closeOnce.Do(a.close)
}

func (a *App) close() {
	a.Logger.Info("Shutting down...")

	var wg sync.WaitGroup

	if a.Messaging != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := a.Messaging.Disconnect(); err != nil {
				a.Logger.Error("Failed to disconnect messaging", "error", err)
			}
		}()
	}

	if a.HTTPServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := a.HTTPServer.Stop(ctx); err != nil {
				a.Logger.Error("Failed to stop http server", "error", err)
			}
		}()
	}

	if a.DB != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if db, err := a.DB.DB(); err != nil {
				a.Logger.Error("Failed to get db instance", "error", err)
			} else if err := db.Close(); err != nil {
				a.Logger.Error("Failed to close db connection", "error", err)
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		a.closeServices()
	}()

	wg.Wait()
	a.Logger.Info("Goodbye!")
}
