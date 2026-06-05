package cache

import (
	"context"
	"testing"
	"time"
)

func TestMemoryCacheSetGetDelete(t *testing.T) {
	ctx := context.Background()
	c := NewMemoryCache()

	if _, found, _ := c.Get(ctx, "missing"); found {
		t.Error("expected miss for unset key")
	}

	if err := c.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, found, err := c.Get(ctx, "k")
	if err != nil || !found || string(got) != "v" {
		t.Fatalf("Get = %q, %v, %v; want v,true,nil", got, found, err)
	}

	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, found, _ := c.Get(ctx, "k"); found {
		t.Error("expected miss after delete")
	}
}

func TestMemoryCacheExpiry(t *testing.T) {
	ctx := context.Background()
	c := NewMemoryCache()
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }

	if err := c.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, found, _ := c.Get(ctx, "k"); !found {
		t.Fatal("entry should be present before expiry")
	}
	now = now.Add(2 * time.Minute)
	if _, found, _ := c.Get(ctx, "k"); found {
		t.Error("entry should be expired")
	}
}

func TestCacheJSONHelpers(t *testing.T) {
	ctx := context.Background()
	c := NewMemoryCache()

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
