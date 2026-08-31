// Package limitx bounds concurrency. Its one type, Keyed, caps how much work
// runs at once per key rather than in total, which is what keeps one slow
// tenant, customer or partner from consuming a shared worker pool.
package limitx

import (
	"context"
	"sync"

	"github.com/hatami57/microjet/core/errorx"
)

// Keyed caps concurrent operations per key.
//
// A global worker pool is fair only while every unit of work costs about the
// same. Once the work talks to something the key controls — a customer's SMTP
// server, a partner's API, a tenant's database — one slow endpoint holds every
// worker it is given, and a pool of N is starved by a single key. Capping per
// key converts that outage into a queue for the key at fault.
//
//	limiter := limitx.NewKeyed[uuid.UUID](2)
//
//	release, err := limiter.Acquire(ctx, tenantID)
//	if err != nil {
//	    return err // the caller's context ended while queueing
//	}
//	defer release()
//
// Keyed holds no state for an idle key, so the number of distinct keys it has
// seen does not accumulate: entries appear on first use and are dropped when
// the last holder releases. The zero value is not usable; call NewKeyed. It is
// safe for concurrent use.
type Keyed[K comparable] struct {
	perKey int

	mu    sync.Mutex
	slots map[K]*slot
}

// slot is one key's semaphore, together with the number of goroutines holding
// or waiting for it. The count is what lets an idle key's entry be removed:
// deleting on release alone would drop a semaphore that waiters are queued on.
type slot struct {
	sem  chan struct{}
	refs int
}

// NewKeyed returns a limiter allowing perKey concurrent operations per key.
// A perKey below 1 is raised to 1, since a limiter that admits nothing would
// deadlock every caller rather than report a misconfiguration.
func NewKeyed[K comparable](perKey int) *Keyed[K] {
	if perKey < 1 {
		perKey = 1
	}
	return &Keyed[K]{perKey: perKey, slots: map[K]*slot{}}
}

// Acquire blocks until the key has a free slot or ctx ends, and returns the
// function that gives the slot back.
//
// The returned release is safe to call more than once and must be called
// exactly once on the success path — defer it. On error it is nil and nothing
// was acquired.
//
// Waiting is not ordered: a caller can be overtaken. Where a slow key must not
// make callers wait at all, give ctx a deadline and treat the error as a signal
// to shed the work rather than queue for it.
func (l *Keyed[K]) Acquire(ctx context.Context, key K) (func(), error) {
	// Fail closed on an already-cancelled context, rather than racing the
	// select below into taking a slot the caller cannot use.
	if err := ctx.Err(); err != nil {
		return nil, errorx.NewInternalError("limitx", "context ended before acquiring").WithInner(err)
	}

	s := l.reserve(key)

	select {
	case s.sem <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() {
				<-s.sem
				l.release(key)
			})
		}, nil
	case <-ctx.Done():
		l.release(key)
		return nil, errorx.NewInternalError("limitx", "timed out waiting for a slot").WithInner(ctx.Err())
	}
}

// reserve returns the key's slot, creating it if needed, and records that one
// more goroutine depends on it.
func (l *Keyed[K]) reserve(key K) *slot {
	l.mu.Lock()
	defer l.mu.Unlock()

	s, ok := l.slots[key]
	if !ok {
		s = &slot{sem: make(chan struct{}, l.perKey)}
		l.slots[key] = s
	}
	s.refs++
	return s
}

// release drops one reference to the key's slot, forgetting the key entirely
// once nothing holds or waits for it.
func (l *Keyed[K]) release(key K) {
	l.mu.Lock()
	defer l.mu.Unlock()

	s, ok := l.slots[key]
	if !ok {
		return
	}
	s.refs--
	if s.refs <= 0 {
		delete(l.slots, key)
	}
}

// Keys reports how many keys currently hold or await a slot. It is meant for
// tests and metrics; a caller cannot act on it without racing.
func (l *Keyed[K]) Keys() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.slots)
}
