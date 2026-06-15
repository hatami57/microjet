package cache

import (
	"context"
	"testing"
	"time"

	"github.com/hatami57/microjet/core"
)

func TestMemoryCacheSetGetDelete(t *testing.T) {
	ctx := context.Background()
	c := NewMemoryCache(core.UTC)

	if _, found, _ := c.GetBytes(ctx, "missing"); found {
		t.Error("expected miss for unset key")
	}

	if err := c.SetBytes(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("SetBytes: %v", err)
	}
	got, found, err := c.GetBytes(ctx, "k")
	if err != nil || !found || string(got) != "v" {
		t.Fatalf("GetBytes = %q, %v, %v; want v,true,nil", got, found, err)
	}

	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, found, _ := c.GetBytes(ctx, "k"); found {
		t.Error("expected miss after delete")
	}
}

func TestMemoryCacheAnyValue(t *testing.T) {
	ctx := context.Background()
	c := NewMemoryCache(core.UTC)

	type point struct{ X, Y int }
	if err := c.Set(ctx, "p", point{X: 1, Y: 2}, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, found, err := c.Get(ctx, "p")
	if err != nil || !found {
		t.Fatalf("Get: found=%v err=%v", found, err)
	}
	// The value round-trips with its original concrete type, no serialization.
	if p, ok := got.(point); !ok || p != (point{X: 1, Y: 2}) {
		t.Errorf("Get = %#v, want point{1 2}", got)
	}

	if _, found, _ := c.Get(ctx, "absent"); found {
		t.Error("expected miss for absent key")
	}
}

func TestMemoryCacheExpiry(t *testing.T) {
	ctx := context.Background()
	clock := core.NewFixedClock(time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC))
	c := NewMemoryCache(clock)

	if err := c.SetBytes(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("SetBytes: %v", err)
	}
	if _, found, _ := c.GetBytes(ctx, "k"); !found {
		t.Fatal("entry should be present before expiry")
	}
	clock.Advance(2 * time.Minute)
	if _, found, _ := c.GetBytes(ctx, "k"); found {
		t.Error("entry should be expired")
	}
}

func TestCacheJSONHelpers(t *testing.T) {
	ctx := context.Background()
	c := NewMemoryCache(core.UTC)

	type user struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := SetJSON(ctx, c, "u", user{ID: 1, Name: "bob"}, 0); err != nil {
		t.Fatalf("SetJSON: %v", err)
	}
	got, found, err := GetJSON[user](ctx, c, "u")
	if err != nil || !found {
		t.Fatalf("GetJSON: found=%v err=%v", found, err)
	}
	if got.ID != 1 || got.Name != "bob" {
		t.Errorf("GetJSON = %+v, want {1 bob}", got)
	}

	if _, found, _ := GetJSON[user](ctx, c, "absent"); found {
		t.Error("expected miss for absent key")
	}
}
