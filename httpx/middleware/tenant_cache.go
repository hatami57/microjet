package middleware

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hatami57/microjet/core"
)

// cachedTenantEntry is a single cache slot. A nil tenant records a negative
// result (the store reported "not found") so repeated lookups for an unknown
// tenant don't hammer the underlying store.
type cachedTenantEntry struct {
	tenant    *TenantBase
	expiresAt time.Time
}

// CachedTenantStore wraps a TenantStore with an in-memory, time-bounded cache.
// It satisfies TenantStore itself, so it can be passed directly to Tenant():
//
//	store := middleware.NewCachedTenantStore(dbStore, 5*time.Minute)
//	router.Use(middleware.Tenant(store))
//
// Both successful and "not found" lookups are cached for the configured TTL.
// Call Invalidate when a tenant changes (e.g. is deactivated) to drop its entry
// before the TTL expires. It is safe for concurrent use.
type CachedTenantStore struct {
	store TenantStore
	ttl   time.Duration
	clock core.TimeProvider
	cache sync.Map // map[uuid.UUID]cachedTenantEntry
}

// CachedTenantOption customizes a CachedTenantStore.
type CachedTenantOption func(*CachedTenantStore)

// WithTenantCacheClock injects the time source used for TTL expiry, allowing
// deterministic tests with a *core.FixedClock. Defaults to core.UTC.
func WithTenantCacheClock(clock core.TimeProvider) CachedTenantOption {
	return func(c *CachedTenantStore) { c.clock = clock }
}

// NewCachedTenantStore returns a CachedTenantStore that caches lookups from the
// given store for ttl.
func NewCachedTenantStore(store TenantStore, ttl time.Duration, opts ...CachedTenantOption) *CachedTenantStore {
	c := &CachedTenantStore{store: store, ttl: ttl, clock: core.UTC}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Find returns the tenant for id, serving from cache when a non-expired entry
// exists (including cached negative results) and otherwise delegating to the
// wrapped store and caching the outcome.
func (c *CachedTenantStore) Find(ctx context.Context, id uuid.UUID) (*TenantBase, error) {
	if v, ok := c.cache.Load(id); ok {
		entry := v.(cachedTenantEntry)
		if c.clock.Now().Before(entry.expiresAt) {
			return entry.tenant, nil
		}
		c.cache.Delete(id)
	}

	tenant, err := c.store.Find(ctx, id)
	if err != nil {
		return nil, err
	}

	c.cache.Store(id, cachedTenantEntry{tenant: tenant, expiresAt: c.clock.Now().Add(c.ttl)})
	return tenant, nil
}

// Invalidate removes the cached entry for id, forcing the next Find to consult
// the wrapped store.
func (c *CachedTenantStore) Invalidate(id uuid.UUID) {
	c.cache.Delete(id)
}
