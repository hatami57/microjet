package host

import (
	"context"
	"testing"
	"time"

	"github.com/hatami57/microjet/cache"
)

func TestWithCacheDefaultsToMemory(t *testing.T) {
	app := newTestApp(t)
	app.WithCache().InitServices()
	if err := app.Err(); err != nil {
		t.Fatalf("WithCache: %v", err)
	}
	c := app.Cache()
	if c == nil {
		t.Fatal("expected a cache to be registered")
	}
	ctx := context.Background()
	if err := c.SetBytes(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("SetBytes: %v", err)
	}
	got, found, err := c.GetBytes(ctx, "k")
	if err != nil || !found || string(got) != "v" {
		t.Fatalf("GetBytes = %q, %v, %v", got, found, err)
	}
	t.Cleanup(app.Close)
}

func TestWithCacheClientInjects(t *testing.T) {
	app := newTestApp(t)
	mem := cache.NewMemoryCache(app.Clock)
	app.WithCacheClient(mem).InitServices()
	if err := app.Err(); err != nil {
		t.Fatalf("WithCacheClient: %v", err)
	}
	if app.Cache() != mem {
		t.Fatal("expected the injected cache to be returned by Cache()")
	}
	t.Cleanup(app.Close)
}

func TestCacheBeforeInitIsNil(t *testing.T) {
	app := newTestApp(t)
	app.WithCache()
	if app.Cache() != nil {
		t.Fatal("expected Cache() to be nil before services are initialized")
	}
}

func TestCacheServiceHealthy(t *testing.T) {
	app := newTestApp(t)
	app.WithCache().InitServices()
	svc, ok := ResolveService[*cacheService](app)
	if !ok {
		t.Fatal("cache service not registered")
	}
	if err := svc.Healthy(context.Background()); err != nil {
		t.Fatalf("memory cache should be healthy: %v", err)
	}
	t.Cleanup(app.Close)
}
