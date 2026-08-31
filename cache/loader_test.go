package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hatami57/microjet/core"
)

type tenantConfig struct {
	Name string `json:"name"`
}

func TestLoaderLoadsOnceAndServesFromCache(t *testing.T) {
	loader := NewLoader[*tenantConfig](NewMemoryCache(core.UTC), time.Minute)

	var calls atomic.Int32
	load := func(context.Context) (*tenantConfig, error) {
		calls.Add(1)
		return &tenantConfig{Name: "acme"}, nil
	}

	for range 3 {
		got, err := loader.Get(t.Context(), "t1", load)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Name != "acme" {
			t.Fatalf("Name = %q, want %q", got.Name, "acme")
		}
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("loader ran %d times, want 1", n)
	}
}

// The reason this type exists. Eight sweeper goroutines hitting one cold key
// must produce one load, not eight.
func TestLoaderCollapsesConcurrentMisses(t *testing.T) {
	loader := NewLoader[*tenantConfig](NewMemoryCache(core.UTC), time.Minute)

	const goroutines = 8
	var (
		calls   atomic.Int32
		release = make(chan struct{})
		ready   sync.WaitGroup
		done    sync.WaitGroup
	)
	ready.Add(goroutines)
	done.Add(goroutines)

	for range goroutines {
		go func() {
			defer done.Done()
			ready.Done()
			_, err := loader.Get(t.Context(), "t1", func(context.Context) (*tenantConfig, error) {
				calls.Add(1)
				<-release // hold the flight open so the others pile up behind it
				return &tenantConfig{Name: "acme"}, nil
			})
			if err != nil {
				t.Errorf("Get: %v", err)
			}
		}()
	}

	ready.Wait()
	// Give the goroutines a moment to reach Get before the load returns;
	// without the pause the first could finish before the rest even start,
	// which would pass for the wrong reason.
	time.Sleep(20 * time.Millisecond)
	close(release)
	done.Wait()

	if n := calls.Load(); n != 1 {
		t.Errorf("loader ran %d times for one key, want 1", n)
	}
}

func TestLoaderDoesNotSerialiseDifferentKeys(t *testing.T) {
	loader := NewLoader[*tenantConfig](NewMemoryCache(core.UTC), time.Minute)

	// Each key's load blocks until the other has started, so the test deadlocks
	// (and fails on timeout) if one key's flight blocks another's.
	first, second := make(chan struct{}), make(chan struct{})
	var done sync.WaitGroup
	done.Add(2)

	go func() {
		defer done.Done()
		_, _ = loader.Get(t.Context(), "a", func(context.Context) (*tenantConfig, error) {
			close(first)
			<-second
			return &tenantConfig{Name: "a"}, nil
		})
	}()
	go func() {
		defer done.Done()
		<-first
		_, _ = loader.Get(t.Context(), "b", func(context.Context) (*tenantConfig, error) {
			close(second)
			return &tenantConfig{Name: "b"}, nil
		})
	}()

	done.Wait()
}

func TestLoaderDoesNotCacheAFailedLoad(t *testing.T) {
	loader := NewLoader[*tenantConfig](NewMemoryCache(core.UTC), time.Minute)
	sentinel := errors.New("database is down")

	var calls atomic.Int32
	load := func(context.Context) (*tenantConfig, error) {
		calls.Add(1)
		return nil, sentinel
	}

	for range 2 {
		_, err := loader.Get(t.Context(), "t1", load)
		if !errors.Is(err, sentinel) {
			t.Fatalf("err = %v, want the loader's own error", err)
		}
	}
	// A cached failure would keep a recovered dependency looking broken for the
	// whole TTL.
	if n := calls.Load(); n != 2 {
		t.Errorf("loader ran %d times, want it retried after the failure", n)
	}
}

// A tenant with nothing configured is the common case, and it has to be
// cacheable or it costs a lookup on every request forever.
func TestLoaderCachesAKnownAbsentValue(t *testing.T) {
	loader := NewLoader[*tenantConfig](NewMemoryCache(core.UTC), time.Minute)

	var calls atomic.Int32
	load := func(context.Context) (*tenantConfig, error) {
		calls.Add(1)
		return nil, nil
	}

	for range 3 {
		got, err := loader.Get(t.Context(), "t1", load)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got != nil {
			t.Fatalf("got = %v, want nil", got)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("loader ran %d times, want the nil result to have been cached", n)
	}
}

func TestLoaderReloadsAfterTTL(t *testing.T) {
	clock := core.NewFixedClock(time.Now().UTC())
	loader := NewLoader[*tenantConfig](NewMemoryCache(clock), time.Minute)

	var calls atomic.Int32
	load := func(context.Context) (*tenantConfig, error) {
		calls.Add(1)
		return &tenantConfig{Name: "acme"}, nil
	}

	if _, err := loader.Get(t.Context(), "t1", load); err != nil {
		t.Fatalf("Get: %v", err)
	}
	clock.Advance(2 * time.Minute)
	if _, err := loader.Get(t.Context(), "t1", load); err != nil {
		t.Fatalf("Get: %v", err)
	}

	if n := calls.Load(); n != 2 {
		t.Errorf("loader ran %d times, want the entry to have expired", n)
	}
}

func TestLoaderInvalidateForcesAReload(t *testing.T) {
	loader := NewLoader[*tenantConfig](NewMemoryCache(core.UTC), time.Minute)

	var calls atomic.Int32
	load := func(context.Context) (*tenantConfig, error) {
		calls.Add(1)
		return &tenantConfig{Name: "acme"}, nil
	}

	if _, err := loader.Get(t.Context(), "t1", load); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := loader.Invalidate(t.Context(), "t1"); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	if _, err := loader.Get(t.Context(), "t1", load); err != nil {
		t.Fatalf("Get: %v", err)
	}

	if n := calls.Load(); n != 2 {
		t.Errorf("loader ran %d times, want the invalidated entry to have reloaded", n)
	}
}

// A live client cannot be serialised, so a Loader must hand back the very value
// the loader produced rather than a copy of it.
func TestLoaderKeepsTheIdenticalValue(t *testing.T) {
	loader := NewLoader[*tenantConfig](NewMemoryCache(core.UTC), time.Minute)
	want := &tenantConfig{Name: "acme"}

	load := func(context.Context) (*tenantConfig, error) { return want, nil }
	if _, err := loader.Get(t.Context(), "t1", load); err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := loader.Get(t.Context(), "t1", load)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != want {
		t.Error("cached value is not the one the loader produced")
	}
}

func TestLoaderTreatsAForeignValueAsAMiss(t *testing.T) {
	c := NewMemoryCache(core.UTC)
	if err := c.Set(t.Context(), "t1", "not a tenantConfig", time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	loader := NewLoader[*tenantConfig](c, time.Minute)

	got, err := loader.Get(t.Context(), "t1", func(context.Context) (*tenantConfig, error) {
		return &tenantConfig{Name: "acme"}, nil
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil || got.Name != "acme" {
		t.Errorf("got = %v, want the entry to have been overwritten rather than erroring", got)
	}
}

func TestJSONLoaderRoundTrips(t *testing.T) {
	loader := NewJSONLoader[tenantConfig](NewMemoryCache(core.UTC), time.Minute)

	var calls atomic.Int32
	load := func(context.Context) (tenantConfig, error) {
		calls.Add(1)
		return tenantConfig{Name: "acme"}, nil
	}

	for range 2 {
		got, err := loader.Get(t.Context(), "t1", load)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Name != "acme" {
			t.Fatalf("Name = %q, want %q", got.Name, "acme")
		}
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("loader ran %d times, want 1", n)
	}
}
