package host

import (
	"context"
	"sync/atomic"
	"testing"
)

// countingWorker implements AsyncWorker and records how many times Go is invoked.
type countingWorker struct {
	count atomic.Int32
}

func (w *countingWorker) Go(ctx context.Context, _ *App) error {
	w.count.Add(1)
	<-ctx.Done()
	return nil
}

func TestDIAsyncWorkerStartsExactlyOnce(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := &countingWorker{}
	ProvideService[*countingWorker](app, w)

	ctx, cancel := context.WithCancel(context.Background())
	wg := app.startWorkers(ctx)
	cancel()
	wg.Wait()

	if got := w.count.Load(); got != 1 {
		t.Errorf("worker Go invoked %d times, want exactly 1", got)
	}
}
