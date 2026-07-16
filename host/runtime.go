package host

import (
	"context"
	"fmt"
)

// Start brings the app fully up — init, setup, start, workers — without
// blocking. The supplied ctx becomes the root of the app's worker context;
// cancelling it begins graceful shutdown, which Wait observes. Start runs the
// same phases as Run and wraps their errors identically ("initializing
// services: ...", etc.), closing the app if a phase fails. It is guarded, so
// calling it more than once is a no-op that returns the original result.
//
// Use Start with Wait and Shutdown to embed an App in a process that already
// owns cancellation — a monolith, CLI, test, or supervisor — where Run's
// built-in signal handling and blocking are not wanted.
func (a *App) Start(ctx context.Context) error {
	a.startOnce.Do(func() { a.startErr = a.start(ctx) })
	return a.startErr
}

// start runs the four startup phases and records the worker context, wait
// group, and merged fatal channel that Wait and Shutdown rely on. It mirrors
// Run's historical phase order and error wrapping exactly.
func (a *App) start(ctx context.Context) error {
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

	workerCtx, cancel := context.WithCancel(ctx)
	a.workerCtx = workerCtx
	a.workerCancel = cancel
	a.workerWg = a.startWorkers(workerCtx)
	// startServices started any background listeners (e.g. the HTTP server's
	// goroutine); their ErrSource channels are merged so Wait reacts to any such
	// service exiting the same way it reacts to a cancelled context.
	a.fatalCh = a.fatalErrCh()
	return nil
}

// Wait blocks until the app begins stopping: the Start context is cancelled, a
// service's background loop exits (an ErrSource delivery), or Shutdown is
// called. It returns the fatal service error, if any — nil for a
// context-cancellation or Shutdown-initiated stop. Repeated calls return the
// same value. Wait does not itself stop the app; pair it with Shutdown.
func (a *App) Wait() error {
	a.waitOnce.Do(func() {
		// A nil worker context (Wait called before Start) yields a nil channel,
		// which blocks forever in the select rather than panicking.
		var workerDone <-chan struct{}
		if a.workerCtx != nil {
			workerDone = a.workerCtx.Done()
		}
		select {
		case <-workerDone:
		case <-a.shutdownCh:
		case err := <-a.fatalCh:
			if err != nil {
				a.Logger.Error("service exited with error", "error", err)
				a.waitErr = err
			} else {
				a.Logger.Warn("service stopped unexpectedly")
			}
		}
	})
	return a.waitErr
}

// Shutdown gracefully stops the app: it unblocks Wait, flips readiness to
// not-ready, waits the configured drain delay (App.ShutdownDelay), cancels the
// workers, waits for them, and closes services. The ctx deadline bounds the
// drain and the wait for workers — cutting a slow drain or a stuck worker short
// — in addition to the WithCloseTimeout that bounds Close. It returns ctx.Err()
// if the ctx expired before the workers stopped, otherwise nil. Safe to call
// more than once; later calls return the first outcome without re-running.
func (a *App) Shutdown(ctx context.Context) error {
	a.shutdownOnce.Do(func() {
		if a.shutdownCh != nil {
			close(a.shutdownCh)
		}
		a.beginShutdown(ctx)
		if a.workerCancel != nil {
			a.workerCancel()
		}
		a.shutdownErr = a.waitWorkers(ctx)
		a.Close()
	})
	return a.shutdownErr
}

// waitWorkers blocks until the worker WaitGroup drains or ctx is done,
// returning ctx.Err() if the deadline cut the wait short. A worker that ignores
// cancellation therefore cannot hang shutdown past the ctx deadline.
func (a *App) waitWorkers(ctx context.Context) error {
	if a.workerWg == nil {
		return nil
	}
	done := make(chan struct{})
	go func() {
		a.workerWg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
