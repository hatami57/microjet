package host

import (
	"context"
	"errors"
	"testing"
	"time"
)

// errSourceService is a fake ErrSource whose background loop's fatal outcome is
// driven by the test through ch.
type errSourceService struct {
	ch chan error
}

func (s *errSourceService) ErrCh() <-chan error { return s.ch }

func newRuntimeApp(t *testing.T) *App {
	t.Helper()
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return app
}

func TestStartShutdownRunsAndStopsWorker(t *testing.T) {
	app := newRuntimeApp(t)

	started := make(chan struct{})
	exited := make(chan struct{})
	app.WithWorker("w", func(ctx context.Context, _ *App) error {
		close(started)
		<-ctx.Done()
		close(exited)
		return nil
	})

	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-started // the worker is running

	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// Shutdown waits for workers, so by the time it returns the worker's context
	// was cancelled and the worker has exited.
	select {
	case <-exited:
	default:
		t.Error("worker had not exited when Shutdown returned")
	}
}

func TestStartContextCancelUnblocksWait(t *testing.T) {
	app := newRuntimeApp(t)

	ctx, cancel := context.WithCancel(context.Background())
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- app.Wait() }()

	// Wait must still be blocked before the cancel.
	select {
	case <-waitDone:
		t.Fatal("Wait returned before the Start context was cancelled")
	case <-time.After(20 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-waitDone:
		if err != nil {
			t.Errorf("Wait() = %v, want nil for a context-cancellation stop", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Wait did not unblock after the Start context was cancelled")
	}

	_ = app.Shutdown(context.Background())
}

func TestWaitReturnsFatalServiceError(t *testing.T) {
	app := newRuntimeApp(t)

	svc := &errSourceService{ch: make(chan error, 1)}
	ProvideService(app, svc)

	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	boom := errors.New("listener crashed")
	svc.ch <- boom

	if err := app.Wait(); !errors.Is(err, boom) {
		t.Errorf("Wait() = %v, want %v", err, boom)
	}
	// Repeated Wait calls report the same fatal error.
	if err := app.Wait(); !errors.Is(err, boom) {
		t.Errorf("second Wait() = %v, want %v", err, boom)
	}

	_ = app.Shutdown(context.Background())
}

func TestShutdownHonorsContextDeadlineWithStuckWorker(t *testing.T) {
	app := newRuntimeApp(t)

	release := make(chan struct{})
	app.WithWorker("stuck", func(ctx context.Context, _ *App) error {
		<-release // deliberately ignores ctx cancellation
		return nil
	})

	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := app.Shutdown(ctx)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Shutdown() = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("Shutdown took %v; it ignored the ctx deadline", elapsed)
	}

	close(release) // let the stuck worker exit so the test leaves nothing running
}

func TestShutdownIsIdempotentAndSafeAfterClose(t *testing.T) {
	app := newRuntimeApp(t)
	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}

	// A Run-style close already happened above; an explicit Close plus a further
	// Shutdown must not panic on a double close.
	app.Close()
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown after Close: %v", err)
	}
}

func TestShutdownFlipsReadiness(t *testing.T) {
	app := newRuntimeApp(t)

	tog := &readyToggler{}
	tog.ready.Store(true)
	ProvideService(app, tog)

	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if tog.ready.Load() {
		t.Error("Shutdown did not flip readiness to not-ready")
	}
}
