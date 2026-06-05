package host

import (
	"context"
	"reflect"
	"sync"
	"time"
)

// AsyncWorker is implemented by DI-registered services that should run as a
// background goroutine. Go is called in a goroutine and should block until
// ctx is cancelled.
type AsyncWorker interface {
	Go(ctx context.Context, app *App) error
}

// PeriodicWorker is implemented by DI-registered services that should run on a
// fixed interval. Go is called immediately on start, then again after each
// GoInterval. The next call never starts until the previous one has returned.
type PeriodicWorker interface {
	GoInterval() time.Duration
	Go(ctx context.Context, app *App) error
}

type worker struct {
	name     string
	fn       func(ctx context.Context, app *App) error
	interval time.Duration // 0 = run once / continuous
}

// WithWorker registers a long-running background goroutine. fn receives a
// context that is cancelled when the app shuts down; fn should return when
// ctx.Done() is closed.
func (a *App) WithWorker(name string, fn func(ctx context.Context, app *App) error) *App {
	a.workers = append(a.workers, worker{name: name, fn: fn})
	return a
}

// WithPeriodicWorker registers a worker that calls fn immediately on start,
// then waits interval before calling again. The next tick never starts until
// the previous call has returned.
func (a *App) WithPeriodicWorker(name string, interval time.Duration, fn func(ctx context.Context, app *App) error) *App {
	a.workers = append(a.workers, worker{name: name, fn: fn, interval: interval})
	return a
}

func (a *App) startWorkers(ctx context.Context) *sync.WaitGroup {
	var wg sync.WaitGroup

	launch := func(name string, interval time.Duration, fn func(context.Context, *App) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.Logger.Info("worker started", "worker", name)
			if interval > 0 {
				a.runPeriodic(ctx, name, interval, fn)
			} else if err := fn(ctx, a); err != nil && ctx.Err() == nil {
				a.Logger.Error("worker exited with error", "worker", name, "error", err)
			}
			a.Logger.Info("worker stopped", "worker", name)
		}()
	}

	for _, w := range a.workers {
		launch(w.name, w.interval, w.fn)
	}

	// Also start any DI-registered services that implement AsyncWorker or PeriodicWorker.
	a.container.Range(func(_, item any) bool {
		name := reflect.TypeOf(item).String()
		if pw, ok := item.(PeriodicWorker); ok {
			interval := pw.GoInterval()
			fn := pw.Go
			launch(name, interval, fn)
		} else if aw, ok := item.(AsyncWorker); ok {
			fn := aw.Go
			launch(name, 0, fn)
		}
		return true
	})

	return &wg
}

func (a *App) runPeriodic(ctx context.Context, name string, interval time.Duration, fn func(context.Context, *App) error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := fn(ctx, a); err != nil && ctx.Err() == nil {
			a.Logger.Error("worker error", "worker", name, "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
