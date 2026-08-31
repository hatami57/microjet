package tenant

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/core/errorx"
)

type cacheEntry struct {
	tenant Tenant
	// err is set on a cached negative result, and holds the not-found error the
	// wrapped store returned. A nil tenant with a nil err would be a hit that
	// hands the caller a nil Tenant inside a non-nil interface — the typed-nil
	// trap — so the two are always stored together.
	err       error
	expiresAt time.Time
}

// Option customizes a CachedStore.
type Option func(*CachedStore)

// WithClock injects the time source used for TTL expiry. Defaults to core.UTC.
// Pass a *core.FixedClock for deterministic tests.
func WithClock(clock core.TimeProvider) Option {
	return func(c *CachedStore) { c.clock = clock }
}

// WithNegativeTTL caches "no such tenant" for ttl, so a stream of requests
// carrying an unknown tenant ID costs one lookup instead of one per request —
// the difference between a misconfigured client being harmless and it being a
// load generator aimed at the tenant store.
//
// It is off by default because it trades freshness for that protection: a
// tenant created after a failed lookup stays invisible to this replica until
// the entry expires. Keep ttl well under the positive TTL, and call Invalidate
// on the create path when the creating replica also serves requests.
//
// Only a not-found error is cached. A store that fails for any other reason — a
// timeout, a dropped connection — is never remembered as an absent tenant.
func WithNegativeTTL(ttl time.Duration) Option {
	return func(c *CachedStore) { c.negativeTTL = ttl }
}

// CachedStore wraps a Store with an in-memory, time-bounded cache.
// It satisfies Store itself, so it can be passed wherever a Store is accepted.
//
//	store := tenant.NewCachedStore(dbStore, 5*time.Minute)
//
// Successful lookups are cached for the configured TTL. "Not found" is cached
// only when WithNegativeTTL asks for it, and never for its own TTL by default.
// Call Invalidate when a tenant changes to drop its entry before the TTL
// expires. It is safe for concurrent use.
type CachedStore struct {
	store       Store
	ttl         time.Duration
	negativeTTL time.Duration
	clock       core.TimeProvider
	cache       sync.Map // map[uuid.UUID]cacheEntry
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
// entry exists (including a negative result, when WithNegativeTTL enabled them)
// and otherwise delegating to the wrapped store and caching the outcome.
func (c *CachedStore) FindTenant(ctx context.Context, id uuid.UUID) (Tenant, error) {
	if v, ok := c.cache.Load(id); ok {
		entry := v.(cacheEntry)
		if c.clock.Now().Before(entry.expiresAt) {
			return entry.tenant, entry.err
		}
		c.cache.Delete(id)
	}

	t, err := c.store.FindTenant(ctx, id)
	if err != nil {
		// Caching any other failure would turn a momentary outage into one that
		// outlives it, reported as a tenant that does not exist.
		if c.negativeTTL > 0 && errorx.IsNotFoundError(err) {
			c.cache.Store(id, cacheEntry{err: err, expiresAt: c.clock.Now().Add(c.negativeTTL)})
		}
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

// Clear drops every cached entry, forcing subsequent FindTenant calls to
// consult the wrapped store. It is safe for concurrent use.
func (c *CachedStore) Clear() {
	c.cache.Clear()
}
