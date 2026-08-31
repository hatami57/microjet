package cache

import (
	"context"
	"time"

	"golang.org/x/sync/singleflight"
)

// Loader is a load-through cache for one kind of value: a miss calls the
// loader, stores the result and returns it, and concurrent misses on the same
// key are collapsed into a single load.
//
// That collapsing is the reason to prefer a Loader over calling Get and Set by
// hand. A background worker that fans out — a sweeper claiming a batch, a
// consumer draining a queue — hits a cold key from every goroutine at once, and
// the hand-written version turns one expiry into a burst of identical database
// reads (or, worse, identical calls to a rate-limited third party).
//
// Build one per kind of value and keep it; the deduplication lives in the
// Loader, so a fresh one per call collapses nothing.
type Loader[T any] struct {
	cache Cache
	ttl   time.Duration
	group singleflight.Group

	get func(ctx context.Context, c Cache, key string) (T, bool, error)
	set func(ctx context.Context, c Cache, key string, v T, ttl time.Duration) error
}

// NewLoader returns a Loader that holds values in c for ttl.
//
// Values are stored through Cache.Get/Set, which keeps them as they are on an
// in-process MemoryCache and gob-encodes them on a Redis one. A value that
// cannot survive that round trip — anything holding a live client, connection
// or credentials cache — belongs in a Loader over a MemoryCache built for the
// purpose, not over whichever driver [cache] happens to name:
//
//	senders := cache.NewLoader[*tenantSender](cache.NewMemoryCache(app.Clock), 5*time.Minute)
//
// Use NewJSONLoader for plain data that should be shared across replicas.
//
// A pointer T is what makes a "known absent" result cacheable: return a nil
// *Config from the loader and the nil is cached like any other value, so a
// tenant with nothing configured stops costing a lookup per request. Returning
// an error instead caches nothing, which is right for a failure and wrong for
// an answer.
func NewLoader[T any](c Cache, ttl time.Duration) *Loader[T] {
	return &Loader[T]{
		cache: c,
		ttl:   ttl,
		get: func(ctx context.Context, c Cache, key string) (T, bool, error) {
			var zero T
			v, found, err := c.Get(ctx, key)
			if err != nil || !found {
				return zero, found, err
			}
			typed, ok := v.(T)
			if !ok {
				// Another kind of value under the same key: treat it as a miss
				// and let the loader overwrite it, rather than failing every
				// call until the entry expires.
				return zero, false, nil
			}
			return typed, true, nil
		},
		set: func(ctx context.Context, c Cache, key string, v T, ttl time.Duration) error {
			return c.Set(ctx, key, v, ttl)
		},
	}
}

// NewJSONLoader returns a Loader that stores values as JSON, so entries are
// readable by every replica sharing a Redis cache. T must round-trip through
// encoding/json: unexported fields, funcs and live clients will not.
func NewJSONLoader[T any](c Cache, ttl time.Duration) *Loader[T] {
	return &Loader[T]{
		cache: c,
		ttl:   ttl,
		get:   func(ctx context.Context, c Cache, key string) (T, bool, error) { return GetJSON[T](ctx, c, key) },
		set: func(ctx context.Context, c Cache, key string, v T, ttl time.Duration) error {
			return SetJSON(ctx, c, key, v, ttl)
		},
	}
}

// Get returns the value cached under key, calling load to produce it on a miss.
//
// A load that fails is not cached and the error is returned as-is, so callers
// can test it with errors.Is. Concurrent misses on one key run load once and
// share its result; misses on different keys do not wait for each other.
//
// The context passed to load belongs to whichever caller won the race to run
// it. Cancelling one caller's context can therefore abandon a load that other
// callers are waiting on — they receive the resulting error. Give load a
// context with its own timeout when that matters.
func (l *Loader[T]) Get(ctx context.Context, key string, load func(context.Context) (T, error)) (T, error) {
	if v, found, err := l.get(ctx, l.cache, key); err == nil && found {
		return v, nil
	}

	v, err, _ := l.group.Do(key, func() (any, error) {
		// The flight may have started while another one was finishing, so look
		// again before paying for a load.
		if v, found, err := l.get(ctx, l.cache, key); err == nil && found {
			return v, nil
		}

		loaded, err := load(ctx)
		if err != nil {
			return nil, err
		}
		if err := l.set(ctx, l.cache, key, loaded, l.ttl); err != nil {
			// The value is good even though caching it failed; returning it
			// keeps a cache outage from becoming an outage.
			return loaded, nil
		}
		return loaded, nil
	})
	if err != nil {
		var zero T
		return zero, err
	}

	typed, ok := v.(T)
	if !ok {
		var zero T
		return zero, nil
	}
	return typed, nil
}

// Invalidate drops the entry for key so the next Get reloads it.
//
// On a shared Redis cache this reaches every replica. On an in-process cache it
// reaches only this one, so a write handled by one replica leaves the others
// serving the old value until it expires — with an in-process cache the TTL is
// the guarantee and Invalidate is an optimisation.
func (l *Loader[T]) Invalidate(ctx context.Context, key string) error {
	return l.cache.Delete(ctx, key)
}
