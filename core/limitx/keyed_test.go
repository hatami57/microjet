package limitx

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestKeyedCapsConcurrencyWithinAKey(t *testing.T) {
	limiter := NewKeyed[string](2)

	var (
		inFlight atomic.Int32
		peak     atomic.Int32
		done     sync.WaitGroup
	)
	for range 10 {
		done.Add(1)
		go func() {
			defer done.Done()
			release, err := limiter.Acquire(t.Context(), "acme")
			if err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			defer release()

			n := inFlight.Add(1)
			for {
				old := peak.Load()
				if n <= old || peak.CompareAndSwap(old, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			inFlight.Add(-1)
		}()
	}
	done.Wait()

	if got := peak.Load(); got > 2 {
		t.Errorf("peak concurrency = %d, want at most 2", got)
	}
}

// The point of the type: a key that holds its slots must not stop other keys
// from working. The test deadlocks and fails on timeout if it does.
func TestKeyedDoesNotBlockOtherKeys(t *testing.T) {
	limiter := NewKeyed[string](1)

	stuck := make(chan struct{})
	held := make(chan struct{})
	var done sync.WaitGroup

	done.Add(1)
	go func() {
		defer done.Done()
		release, err := limiter.Acquire(t.Context(), "slow")
		if err != nil {
			t.Errorf("Acquire: %v", err)
			return
		}
		defer release()
		close(held)
		<-stuck
	}()

	<-held
	release, err := limiter.Acquire(t.Context(), "fast")
	if err != nil {
		t.Fatalf("a second key had to wait for the first: %v", err)
	}
	release()

	close(stuck)
	done.Wait()
}

func TestKeyedAcquireRespectsContextDeadline(t *testing.T) {
	limiter := NewKeyed[string](1)

	release, err := limiter.Acquire(t.Context(), "acme")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	second, err := limiter.Acquire(ctx, "acme")
	if err == nil {
		second()
		t.Fatal("expected the second acquire to give up when the deadline passed")
	}
	if second != nil {
		t.Error("a failed Acquire must not return a release function")
	}
}

func TestKeyedAcquireRejectsAnEndedContext(t *testing.T) {
	limiter := NewKeyed[string](4)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := limiter.Acquire(ctx, "acme"); err == nil {
		t.Fatal("expected an error for an already-cancelled context")
	}
	if got := limiter.Keys(); got != 0 {
		t.Errorf("keys = %d, want the rejected acquire to have left nothing behind", got)
	}
}

// Keys are unbounded in principle — one per tenant, per customer, per partner —
// so an idle key must cost nothing or the limiter becomes the leak.
func TestKeyedForgetsIdleKeys(t *testing.T) {
	limiter := NewKeyed[int](2)

	for i := range 100 {
		release, err := limiter.Acquire(t.Context(), i)
		if err != nil {
			t.Fatalf("Acquire: %v", err)
		}
		release()
	}

	if got := limiter.Keys(); got != 0 {
		t.Errorf("keys retained = %d, want 0 after every holder released", got)
	}
}

func TestKeyedKeepsTheSlotWhileWaitersRemain(t *testing.T) {
	limiter := NewKeyed[string](1)

	first, err := limiter.Acquire(t.Context(), "acme")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	waiting := make(chan struct{})
	acquired := make(chan func(), 1)
	go func() {
		close(waiting)
		release, err := limiter.Acquire(t.Context(), "acme")
		if err != nil {
			t.Errorf("Acquire: %v", err)
			return
		}
		acquired <- release
	}()

	<-waiting
	time.Sleep(20 * time.Millisecond)
	// Releasing the only holder while a waiter is queued must hand the slot
	// over, not delete the semaphore out from under it.
	first()

	select {
	case release := <-acquired:
		release()
	case <-time.After(time.Second):
		t.Fatal("the waiter never acquired the freed slot")
	}

	if got := limiter.Keys(); got != 0 {
		t.Errorf("keys retained = %d, want 0", got)
	}
}

func TestKeyedReleaseIsIdempotent(t *testing.T) {
	limiter := NewKeyed[string](1)

	release, err := limiter.Acquire(t.Context(), "acme")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	release()
	release() // a defer plus an explicit call must not free a slot twice

	// The slot is genuinely free exactly once, so this acquire succeeds and a
	// second concurrent one would not.
	second, err := limiter.Acquire(t.Context(), "acme")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer second()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if third, err := limiter.Acquire(ctx, "acme"); err == nil {
		third()
		t.Error("the double release handed out an extra slot")
	}
}

func TestNewKeyedRaisesAnUnusableLimit(t *testing.T) {
	for _, perKey := range []int{0, -1} {
		limiter := NewKeyed[string](perKey)
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)

		release, err := limiter.Acquire(ctx, "acme")
		if err != nil {
			t.Fatalf("perKey %d deadlocked instead of admitting one caller", perKey)
		}
		release()
		cancel()
	}
}
