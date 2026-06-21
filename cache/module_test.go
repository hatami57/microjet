package cache

import (
	"context"
	"testing"
	"time"

	"github.com/hatami57/microjet/host"
)

func newApp(t *testing.T) *host.App {
	t.Helper()
	app, err := host.New()
	if err != nil {
		t.Fatalf("host.New: %v", err)
	}
	return app
}

func TestModuleDefaultsToMemory(t *testing.T) {
	app := newApp(t)
	app.WithModule(Module()).InitServices()
	if err := app.Err(); err != nil {
		t.Fatalf("Module: %v", err)
	}
	c := Of(app)
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

func TestModuleWithClientInjects(t *testing.T) {
	app := newApp(t)
	mem := NewMemoryCache(app.Clock)
	app.WithModule(ModuleWithClient(mem)).InitServices()
	if err := app.Err(); err != nil {
		t.Fatalf("ModuleWithClient: %v", err)
	}
	if Of(app) != mem {
		t.Fatal("expected the injected cache to be returned by Of()")
	}
	t.Cleanup(app.Close)
}

func TestOfBeforeInitIsNil(t *testing.T) {
	app := newApp(t)
	app.WithModule(Module())
	if Of(app) != nil {
		t.Fatal("expected Of() to be nil before services are initialized")
	}
}

func TestServiceHealthy(t *testing.T) {
	app := newApp(t)
	app.WithModule(Module()).InitServices()
	svc, ok := host.ResolveService[*service](app)
	if !ok {
		t.Fatal("cache service not registered")
	}
	if err := svc.Healthy(context.Background()); err != nil {
		t.Fatalf("memory cache should be healthy: %v", err)
	}
	t.Cleanup(app.Close)
}
