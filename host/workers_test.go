package host

import (
	"context"
	"sync/atomic"
	"testing"
)

// countingWorker implements AsyncWorker and records how many times Run is invoked.
type countingWorker struct {
	count atomic.Int32
}

func (w *countingWorker) Run(ctx context.Context, _ *App) error {
	w.count.Add(1)
	<-ctx.Done()
	return nil
}

func TestWorkerPanicIsRecovered(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ran := make(chan struct{})
	app.WithWorker("panicker", func(_ context.Context, _ *App) error {
		defer close(ran)
		panic("boom")
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wg := app.startWorkers(ctx)

	// If the panic were not recovered the test process would crash; reaching
	// here (and the worker goroutine completing) proves it was contained.
	<-ran
	cancel()
	wg.Wait()
}

func TestDIAsyncWorkerStartsExactlyOnce(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := &countingWorker{}
	ProvideService(app, w)

	ctx, cancel := context.WithCancel(context.Background())
	wg := app.startWorkers(ctx)
	cancel()
	wg.Wait()

	if got := w.count.Load(); got != 1 {
		t.Errorf("worker Run invoked %d times, want exactly 1", got)
	}
}

func TestDINamedWorkersOfOneTypeAllStart(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a, b := &countingWorker{}, &countingWorker{}
	ProvideService(app, a, "a")
	ProvideService(app, b, "b")

	ctx, cancel := context.WithCancel(context.Background())
	wg := app.startWorkers(ctx)
	cancel()
	wg.Wait()

	if a.count.Load() != 1 || b.count.Load() != 1 {
		t.Errorf("worker Run invocations: a=%d b=%d, want 1 each", a.count.Load(), b.count.Load())
	}
}

func TestDIWorkerProvidedUnderTwoKeysStartsOnce(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := &countingWorker{}
	ProvideService(app, w)
	ProvideService[AsyncWorker](app, w)

	ctx, cancel := context.WithCancel(context.Background())
	wg := app.startWorkers(ctx)
	cancel()
	wg.Wait()

	if got := w.count.Load(); got != 1 {
		t.Errorf("worker Run invoked %d times, want exactly 1", got)
	}
}
