package tenant

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/core/errorx"
)

// stubStore counts lookups so a test can tell a cache hit from a miss.
type stubStore struct {
	calls  int
	tenant Tenant
	err    error
}

func (s *stubStore) FindTenant(context.Context, uuid.UUID) (Tenant, error) {
	s.calls++
	return s.tenant, s.err
}

func newTenant(code string) Tenant {
	return &Base{ID: uuid.New(), Code: code, IsActive: true}
}

func TestCachedStoreServesRepeatLookupsFromCache(t *testing.T) {
	store := &stubStore{tenant: newTenant("acme")}
	cached := NewCachedStore(store, time.Minute)
	id := uuid.New()

	for range 3 {
		got, err := cached.FindTenant(t.Context(), id)
		if err != nil {
			t.Fatalf("FindTenant: %v", err)
		}
		if got.AsBase().Code != "acme" {
			t.Fatalf("Code = %q, want %q", got.AsBase().Code, "acme")
		}
	}
	if store.calls != 1 {
		t.Errorf("store was consulted %d times, want 1", store.calls)
	}
}

func TestCachedStoreReloadsAfterTTL(t *testing.T) {
	store := &stubStore{tenant: newTenant("acme")}
	clock := core.NewFixedClock(time.Now().UTC())
	cached := NewCachedStore(store, time.Minute, WithClock(clock))
	id := uuid.New()

	if _, err := cached.FindTenant(t.Context(), id); err != nil {
		t.Fatalf("FindTenant: %v", err)
	}
	clock.Advance(2 * time.Minute)
	if _, err := cached.FindTenant(t.Context(), id); err != nil {
		t.Fatalf("FindTenant: %v", err)
	}

	if store.calls != 2 {
		t.Errorf("store was consulted %d times, want the entry to have expired", store.calls)
	}
}

// Without WithNegativeTTL the cache must stay out of the way of a missing
// tenant, so one created moments later is visible at once.
func TestCachedStoreDoesNotCacheNotFoundByDefault(t *testing.T) {
	store := &stubStore{err: errorx.NewNotFoundError("tenant", "no such tenant")}
	cached := NewCachedStore(store, time.Minute)
	id := uuid.New()

	for range 3 {
		if _, err := cached.FindTenant(t.Context(), id); !errorx.IsNotFoundError(err) {
			t.Fatalf("err = %v, want a not-found error", err)
		}
	}
	if store.calls != 3 {
		t.Errorf("store was consulted %d times, want every lookup to have reached it", store.calls)
	}
}

func TestCachedStoreCachesNotFoundWhenAsked(t *testing.T) {
	store := &stubStore{err: errorx.NewNotFoundError("tenant", "no such tenant")}
	cached := NewCachedStore(store, time.Minute, WithNegativeTTL(10*time.Second))
	id := uuid.New()

	for range 3 {
		got, err := cached.FindTenant(t.Context(), id)
		if !errorx.IsNotFoundError(err) {
			t.Fatalf("err = %v, want the store's not-found error", err)
		}
		// A cached negative must not hand back a nil Tenant with a nil error:
		// a typed nil inside a non-nil interface passes a != nil check and
		// panics at the first method call.
		if got != nil {
			t.Fatalf("tenant = %v, want nil alongside the error", got)
		}
	}
	if store.calls != 1 {
		t.Errorf("store was consulted %d times, want 1", store.calls)
	}
}

// A timeout is not evidence that a tenant does not exist. Caching it would make
// a blip outlive itself, reported as a 404 for the whole TTL.
func TestCachedStoreNeverCachesOtherFailures(t *testing.T) {
	store := &stubStore{err: errors.New("connection reset")}
	cached := NewCachedStore(store, time.Minute, WithNegativeTTL(10*time.Second))
	id := uuid.New()

	for range 3 {
		if _, err := cached.FindTenant(t.Context(), id); err == nil {
			t.Fatal("expected the store's error")
		}
	}
	if store.calls != 3 {
		t.Errorf("store was consulted %d times, want every lookup to have retried", store.calls)
	}
}

func TestCachedStoreNegativeEntryExpiresOnItsOwnTTL(t *testing.T) {
	store := &stubStore{err: errorx.NewNotFoundError("tenant", "no such tenant")}
	clock := core.NewFixedClock(time.Now().UTC())
	cached := NewCachedStore(store, time.Hour, WithClock(clock), WithNegativeTTL(10*time.Second))
	id := uuid.New()

	if _, err := cached.FindTenant(t.Context(), id); !errorx.IsNotFoundError(err) {
		t.Fatalf("err = %v, want a not-found error", err)
	}

	// The tenant is created in the meantime. The short negative TTL is what
	// bounds how long this replica keeps denying it.
	clock.Advance(30 * time.Second)
	store.err = nil
	store.tenant = newTenant("acme")

	got, err := cached.FindTenant(t.Context(), id)
	if err != nil {
		t.Fatalf("FindTenant: %v", err)
	}
	if got.AsBase().Code != "acme" {
		t.Errorf("Code = %q, want the tenant created since", got.AsBase().Code)
	}
}

func TestCachedStoreInvalidateAndClear(t *testing.T) {
	store := &stubStore{tenant: newTenant("acme")}
	cached := NewCachedStore(store, time.Minute)
	id := uuid.New()

	if _, err := cached.FindTenant(t.Context(), id); err != nil {
		t.Fatalf("FindTenant: %v", err)
	}
	cached.Invalidate(id)
	if _, err := cached.FindTenant(t.Context(), id); err != nil {
		t.Fatalf("FindTenant: %v", err)
	}
	if store.calls != 2 {
		t.Fatalf("store was consulted %d times, want Invalidate to have dropped the entry", store.calls)
	}

	cached.Clear()
	if _, err := cached.FindTenant(t.Context(), id); err != nil {
		t.Fatalf("FindTenant: %v", err)
	}
	if store.calls != 3 {
		t.Errorf("store was consulted %d times, want Clear to have dropped every entry", store.calls)
	}
}

func TestCachedStoreInvalidateDropsANegativeEntry(t *testing.T) {
	store := &stubStore{err: errorx.NewNotFoundError("tenant", "no such tenant")}
	cached := NewCachedStore(store, time.Minute, WithNegativeTTL(time.Minute))
	id := uuid.New()

	if _, err := cached.FindTenant(t.Context(), id); !errorx.IsNotFoundError(err) {
		t.Fatalf("err = %v, want a not-found error", err)
	}

	// The create path's escape hatch: the replica that creates a tenant can
	// undo its own cached denial immediately.
	cached.Invalidate(id)
	store.err = nil
	store.tenant = newTenant("acme")

	if _, err := cached.FindTenant(t.Context(), id); err != nil {
		t.Fatalf("FindTenant: %v", err)
	}
}
