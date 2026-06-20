package tenant

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hatami57/microjet/core"
)

type cacheEntry struct {
	tenant    Tenant
	expiresAt time.Time
}

// Option customizes a CachedStore.
type Option func(*CachedStore)

// WithClock injects the time source used for TTL expiry. Defaults to core.UTC.
// Pass a *core.FixedClock for deterministic tests.
func WithClock(clock core.TimeProvider) Option {
	return func(c *CachedStore) { c.clock = clock }
}

// CachedStore wraps a Store with an in-memory, time-bounded cache.
// It satisfies Store itself, so it can be passed wherever a Store is accepted.
//
//	store := tenant.NewCachedStore(dbStore, 5*time.Minute)
//
// Both successful and "not found" lookups are cached for the configured TTL.
// Call Invalidate when a tenant changes to drop its entry before the TTL
// expires. It is safe for concurrent use.
type CachedStore struct {
	store Store
	ttl   time.Duration
	clock core.TimeProvider
	cache sync.Map // map[uuid.UUID]cacheEntry
}

// NewCachedStore returns a CachedStore that caches lookups from store for ttl.
func NewCachedStore(store Store, ttl time.Duration, opts ...Option) *CachedStore {
	c := &CachedStore{store: store, ttl: ttl, clock: core.UTC}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// FindTenant returns the tenant for id, serving from cache when a non-expired
// entry exists (including cached negative results) and otherwise delegating to
// the wrapped store and caching the outcome.
func (c *CachedStore) FindTenant(ctx context.Context, id uuid.UUID) (Tenant, error) {
	if v, ok := c.cache.Load(id); ok {
		entry := v.(cacheEntry)
		if c.clock.Now().Before(entry.expiresAt) {
			return entry.tenant, nil
		}
		c.cache.Delete(id)
	}

	t, err := c.store.FindTenant(ctx, id)
	if err != nil {
		return nil, err
	}

	c.cache.Store(id, cacheEntry{tenant: t, expiresAt: c.clock.Now().Add(c.ttl)})
	return t, nil
}

// Invalidate removes the cached entry for id, forcing the next FindTenant to
// consult the wrapped store.
func (c *CachedStore) Invalidate(id uuid.UUID) {
	c.cache.Delete(id)
}
