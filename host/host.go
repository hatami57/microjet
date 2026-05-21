package host

import (
	"context"
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

	container            sync.Map
	isServiceInitialized bool
}

type HandlerFunc func(app *App) error

func New() *App {
	return NewWithEnvPrefix("")
}

func NewWithEnvPrefix(envPrefix string) *App {
	config := &core.Config{}
	if err := core.Load(config, envPrefix); err != nil {
		panic(err)
	}
	return &App{
		Config: config,
		Logger: core.NewLogger(config.Log),
	}
}

func WaitForExitSignal() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
}

func (a *App) MustRun(handler HandlerFunc) {
	if err := handler(a); err != nil {
		a.Logger.Error("handler failed", "error", err)
		os.Exit(1)
	}
}

func (a *App) Close() {
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
