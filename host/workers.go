package host

import (
	"context"
	"reflect"
	"runtime/debug"
	"sync"
	"time"

	"github.com/hatami57/microjet/core/errorx"
)

// AsyncWorker is implemented by DI-registered services that should run as a
// background goroutine. Run is called in a goroutine and should block until
// ctx is cancelled.
type AsyncWorker interface {
	Run(ctx context.Context, app *App) error
}

// PeriodicWorker is implemented by DI-registered services that should run on a
// fixed interval. Run is called immediately on start, then again after each
// Interval. The next call never starts until the previous one has returned.
type PeriodicWorker interface {
	Interval() time.Duration
	Run(ctx context.Context, app *App) error
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
		wg.Go(func() {
			a.Logger.Info("worker started", "worker", name)
			if interval > 0 {
				a.runPeriodic(ctx, name, interval, fn)
			} else if err := a.safeRun(ctx, name, fn); err != nil && ctx.Err() == nil {
				a.Logger.Error("worker exited with error", "worker", name, "error", err)
			}
			a.Logger.Info("worker stopped", "worker", name)
		})
	}

	// Explicitly registered workers first, in registration order.
	for _, w := range a.workers {
		launch(w.name, w.interval, w.fn)
	}

	// Then DI-registered services implementing AsyncWorker/PeriodicWorker, in
	// registration order (orderedRange); PeriodicWorker takes precedence over
	// AsyncWorker. A service provided under several keys (say, its concrete type
	// and an interface) is one worker, so services are deduplicated by identity —
	// the pointer for pointer types, the value for other comparable ones — and
	// each starts once. Distinct instances of one type, provided side by side
	// under different names, each start.
	var diWorkers []worker
	seen := make(map[any]bool)
	a.orderedRange(func(key, item any) bool {
		if _, ok := item.(AsyncWorker); !ok {
			return true
		}
		if reflect.ValueOf(item).Comparable() {
			if seen[item] {
				return true
			}
			seen[item] = true
		}
		name := reflect.TypeOf(item).String()
		if k, ok := key.(serviceKey); ok && k.name != "" {
			name += " (name " + k.name + ")"
		}
		switch w := item.(type) {
		case PeriodicWorker:
			diWorkers = append(diWorkers, worker{name: name, fn: w.Run, interval: w.Interval()})
		case AsyncWorker:
			diWorkers = append(diWorkers, worker{name: name, fn: w.Run})
		}
		return true
	})
	for _, w := range diWorkers {
		launch(w.name, w.interval, w.fn)
	}

	return &wg
}

func (a *App) runPeriodic(ctx context.Context, name string, interval time.Duration, fn func(context.Context, *App) error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		// A panic or error on one tick is logged but never stops the ticker:
		// periodic workers keep running on schedule.
		if err := a.safeRun(ctx, name, fn); err != nil && ctx.Err() == nil {
			a.Logger.Error("worker error", "worker", name, "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// safeRun invokes a worker function, converting a panic into an error so a
// crashing worker is logged (with its stack) instead of taking down the whole
// process. A one-shot worker that panics ends; a periodic worker keeps ticking.
func (a *App) safeRun(ctx context.Context, name string, fn func(context.Context, *App) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			a.Logger.Error("worker panic recovered", "worker", name, "panic", r, "stack", string(debug.Stack()))
			err = errorx.NewInternalError("host", "worker panicked", "worker", name, "panic", r)
		}
	}()
	return fn(ctx, a)
}
