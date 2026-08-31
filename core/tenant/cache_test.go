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

// A Store may spell "no such tenant" either way round. Both are the same
// answer and must be cached alike — the (nil, nil) form especially, since it is
// what middleware.Tenant turns into a 401 and so the one most stores use.
func TestCachedStoreCachesAnAbsentTenantEitherSpelling(t *testing.T) {
	notFound := errorx.NewNotFoundError("tenant", "no such tenant")

	tests := []struct {
		name   string
		store  *stubStore
		verify func(t *testing.T, got Tenant, err error)
	}{
		{
			name:  "reported as (nil, nil)",
			store: &stubStore{},
			verify: func(t *testing.T, got Tenant, err error) {
				if got != nil || err != nil {
					t.Fatalf("got (%v, %v), want the store's own (nil, nil)", got, err)
				}
			},
		},
		{
			name:  "reported as a not-found error",
			store: &stubStore{err: notFound},
			verify: func(t *testing.T, got Tenant, err error) {
				if !errorx.IsNotFoundError(err) {
					t.Fatalf("err = %v, want the store's not-found error", err)
				}
				// Never a nil Tenant with a nil error here: the caller
				// distinguishes the two, and the cache must not blur them.
				if got != nil {
					t.Fatalf("tenant = %v, want nil alongside the error", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cached := NewCachedStore(tt.store, time.Minute)
			id := uuid.New()

			for range 3 {
				got, err := cached.FindTenant(t.Context(), id)
				tt.verify(t, got, err)
			}
			if tt.store.calls != 1 {
				t.Errorf("store was consulted %d times, want 1", tt.store.calls)
			}
		})
	}
}

// The absence is held for the positive TTL unless told otherwise, so a caller
// that never mentions negative caching still gets it.
func TestCachedStoreAbsenceInheritsThePositiveTTL(t *testing.T) {
	store := &stubStore{}
	clock := core.NewFixedClock(time.Now().UTC())
	cached := NewCachedStore(store, time.Minute, WithClock(clock))
	id := uuid.New()

	if _, err := cached.FindTenant(t.Context(), id); err != nil {
		t.Fatalf("FindTenant: %v", err)
	}
	clock.Advance(30 * time.Second)
	if _, err := cached.FindTenant(t.Context(), id); err != nil {
		t.Fatalf("FindTenant: %v", err)
	}
	if store.calls != 1 {
		t.Fatalf("store was consulted %d times inside the TTL, want 1", store.calls)
	}

	clock.Advance(31 * time.Second)
	if _, err := cached.FindTenant(t.Context(), id); err != nil {
		t.Fatalf("FindTenant: %v", err)
	}
	if store.calls != 2 {
		t.Errorf("store was consulted %d times, want the entry to have expired", store.calls)
	}
}

func TestCachedStoreNegativeTTLZeroDisablesTheCaching(t *testing.T) {
	notFound := errorx.NewNotFoundError("tenant", "no such tenant")

	for _, tt := range []struct {
		name  string
		store *stubStore
	}{
		{"reported as (nil, nil)", &stubStore{}},
		{"reported as a not-found error", &stubStore{err: notFound}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cached := NewCachedStore(tt.store, time.Minute, WithNegativeTTL(0))
			id := uuid.New()

			for range 3 {
				_, _ = cached.FindTenant(t.Context(), id)
			}
			// An explicit zero is a decision, not an unset field: a tenant
			// created moments after a failed lookup must be visible at once.
			if tt.store.calls != 3 {
				t.Errorf("store was consulted %d times, want every lookup to have reached it", tt.store.calls)
			}
		})
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
	for _, tt := range []struct {
		name  string
		store *stubStore
	}{
		{"reported as (nil, nil)", &stubStore{}},
		{"reported as a not-found error", &stubStore{err: errorx.NewNotFoundError("tenant", "gone")}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clock := core.NewFixedClock(time.Now().UTC())
			cached := NewCachedStore(tt.store, time.Hour, WithClock(clock), WithNegativeTTL(10*time.Second))
			id := uuid.New()

			if _, err := cached.FindTenant(t.Context(), id); tt.store.err != nil && err == nil {
				t.Fatal("expected the store's not-found error")
			}

			// The tenant is created in the meantime. The short negative TTL is
			// what bounds how long this replica keeps denying it — a full hour
			// would mean the absence outlived the fix by 59 minutes.
			clock.Advance(30 * time.Second)
			tt.store.err = nil
			tt.store.tenant = newTenant("acme")

			got, err := cached.FindTenant(t.Context(), id)
			if err != nil {
				t.Fatalf("FindTenant: %v", err)
			}
			if got == nil || got.AsBase().Code != "acme" {
				t.Errorf("got = %v, want the tenant created since", got)
			}
		})
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
