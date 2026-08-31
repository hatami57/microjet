package tenant

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/core/errorx"
)

// cacheEntry is one remembered answer, positive or negative. A negative entry
// has a nil tenant, and an err that is nil or not depending on which of the two
// "no such tenant" spellings the wrapped store used — see FindTenant. Both
// fields are replayed together, so a hit reproduces the store's answer exactly.
type cacheEntry struct {
	tenant    Tenant
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

// WithNegativeTTL sets how long "no such tenant" is remembered, which defaults
// to the store's positive TTL.
//
// Caching the absence at all is what keeps a stream of requests carrying an
// unknown tenant ID costing one lookup instead of one per request — the
// difference between a misconfigured client being harmless and it being a load
// generator aimed at the tenant store. Shortening it trades some of that
// protection back for freshness, since a tenant created after a failed lookup
// stays invisible to this replica until the entry expires; a few seconds is
// usually enough to absorb a burst without being noticeable.
//
// Pass 0 to switch negative caching off entirely, and call Invalidate on the
// create path when the creating replica also serves requests.
//
// Only an absent tenant is remembered. A store that fails for any other reason
// — a timeout, a dropped connection — is never cached, whatever this is set to.
func WithNegativeTTL(ttl time.Duration) Option {
	return func(c *CachedStore) {
		c.negativeTTL = ttl
		c.negativeTTLSet = true
	}
}

// CachedStore wraps a Store with an in-memory, time-bounded cache.
// It satisfies Store itself, so it can be passed wherever a Store is accepted.
//
//	store := tenant.NewCachedStore(dbStore, 5*time.Minute)
//
// Both successful and "not found" lookups are cached for the configured TTL;
// WithNegativeTTL gives the second kind a shorter one. Call Invalidate when a
// tenant changes to drop its entry before the TTL expires. It is safe for
// concurrent use.
//
// A wrapped store that returns a *typed* nil — (*YourTenant)(nil) rather than a
// plain nil — is caching a value that is not nil as an interface and will not
// be recognised as an absent tenant, here or by middleware.Tenant. The cache
// makes that bug outlive the request it happened on, so return an untyped nil.
type CachedStore struct {
	store Store
	ttl   time.Duration
	// negativeTTL applies to an absent tenant; negativeTTLSet distinguishes an
	// explicit WithNegativeTTL(0), which disables negative caching, from the
	// option not being passed at all, which inherits ttl.
	negativeTTL    time.Duration
	negativeTTLSet bool
	clock          core.TimeProvider
	cache          sync.Map // map[uuid.UUID]cacheEntry
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
// entry exists — including a remembered absence — and otherwise delegating to
// the wrapped store and caching the outcome.
//
// A Store may report an absent tenant either way round, and both are treated as
// the same answer and held for the same negative TTL:
//
//   - (nil, nil), which is what middleware.Tenant turns into a 401 and is the
//     spelling a Store implementation should prefer;
//   - (nil, errorx.NotFoundError), for a store that surfaces its repository's
//     error directly.
//
// A hit replays whichever one the store gave, so wrapping a Store in a
// CachedStore never changes what its caller sees.
func (c *CachedStore) FindTenant(ctx context.Context, id uuid.UUID) (Tenant, error) {
	if v, ok := c.cache.Load(id); ok {
		entry := v.(cacheEntry)
		if c.clock.Now().Before(entry.expiresAt) {
			return entry.tenant, entry.err
		}
		c.cache.Delete(id)
	}

	t, err := c.store.FindTenant(ctx, id)
	switch {
	case err != nil && errorx.IsNotFoundError(err):
		c.remember(id, cacheEntry{err: err}, c.absentTTL())
		return nil, err

	case err != nil:
		// Any other failure says nothing about whether the tenant exists.
		// Caching it would turn a momentary outage into one that outlives
		// itself, reported as a tenant that is not there.
		return nil, err

	case t == nil:
		c.remember(id, cacheEntry{}, c.absentTTL())
		return nil, nil

	default:
		c.remember(id, cacheEntry{tenant: t}, c.ttl)
		return t, nil
	}
}

// absentTTL is how long a "no such tenant" answer is held: the negative TTL
// when one was set, and the positive TTL otherwise.
func (c *CachedStore) absentTTL() time.Duration {
	if c.negativeTTLSet {
		return c.negativeTTL
	}
	return c.ttl
}

// remember stores entry under id for ttl, and stores nothing at all when ttl is
// not positive — which is how WithNegativeTTL(0) switches negative caching off.
func (c *CachedStore) remember(id uuid.UUID, entry cacheEntry, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	entry.expiresAt = c.clock.Now().Add(ttl)
	c.cache.Store(id, entry)
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
