package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hatami57/microjet/core"
)

// countingStore records how many times Find reaches the underlying store and
// can be told to return a "not found" (nil) result.
type countingStore struct {
	calls    int
	notFound bool
}

func (s *countingStore) Find(_ context.Context, id uuid.UUID) (*TenantBase, error) {
	s.calls++
	if s.notFound {
		return nil, nil
	}
	return &TenantBase{ID: id, IsActive: true}, nil
}

func TestCachedTenantStoreServesFromCache(t *testing.T) {
	store := &countingStore{}
	cached := NewCachedTenantStore(store, time.Minute)
	id := uuid.New()

	for i := 0; i < 3; i++ {
		got, err := cached.Find(context.Background(), id)
		if err != nil {
			t.Fatalf("Find: %v", err)
		}
		if got == nil || got.ID != id {
			t.Fatalf("Find returned %+v, want tenant %s", got, id)
		}
	}

	if store.calls != 1 {
		t.Errorf("underlying store calls = %d, want 1", store.calls)
	}
}

func TestCachedTenantStoreCachesNegativeResult(t *testing.T) {
	store := &countingStore{notFound: true}
	cached := NewCachedTenantStore(store, time.Minute)
	id := uuid.New()

	for i := 0; i < 3; i++ {
		got, err := cached.Find(context.Background(), id)
		if err != nil {
			t.Fatalf("Find: %v", err)
		}
		if got != nil {
			t.Fatalf("Find returned %+v, want nil", got)
		}
	}

	if store.calls != 1 {
		t.Errorf("underlying store calls = %d, want 1 (negative result should be cached)", store.calls)
	}
}

func TestCachedTenantStoreExpiresEntries(t *testing.T) {
	store := &countingStore{}
	clock := core.NewFixedClock(time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC))
	cached := NewCachedTenantStore(store, time.Minute, WithTenantCacheClock(clock))
	id := uuid.New()

	if _, err := cached.Find(context.Background(), id); err != nil {
		t.Fatalf("Find: %v", err)
	}
	// Still within the TTL: served from cache.
	clock.Advance(30 * time.Second)
	if _, err := cached.Find(context.Background(), id); err != nil {
		t.Fatalf("Find: %v", err)
	}
	if store.calls != 1 {
		t.Fatalf("underlying store calls = %d, want 1 (still cached)", store.calls)
	}
	// Past the TTL: entry expired, store consulted again.
	clock.Advance(time.Minute)
	if _, err := cached.Find(context.Background(), id); err != nil {
		t.Fatalf("Find: %v", err)
	}
	if store.calls != 2 {
		t.Errorf("underlying store calls = %d, want 2 (entry should have expired)", store.calls)
	}
}

func TestCachedTenantStoreInvalidate(t *testing.T) {
	store := &countingStore{}
	cached := NewCachedTenantStore(store, time.Minute)
	id := uuid.New()

	if _, err := cached.Find(context.Background(), id); err != nil {
		t.Fatalf("Find: %v", err)
	}
	cached.Invalidate(id)
	if _, err := cached.Find(context.Background(), id); err != nil {
		t.Fatalf("Find: %v", err)
	}

	if store.calls != 2 {
		t.Errorf("underlying store calls = %d, want 2 (Invalidate should drop the entry)", store.calls)
	}
}
