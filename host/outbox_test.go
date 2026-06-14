package host

import (
	"context"
	"testing"

	"github.com/hatami57/microjet/gormx/sqlite"
	"github.com/hatami57/microjet/outbox"
)

// findWorker returns the registered worker function for name.
func findWorker(t *testing.T, app *App, name string) func(context.Context, *App) error {
	t.Helper()
	for _, w := range app.workers {
		if w.name == name {
			return w.fn
		}
	}
	t.Fatalf("worker %q not registered", name)
	return nil
}

func TestWithOutboxMigratesAndRelays(t *testing.T) {
	broker := newFakeBroker()
	app := newTestApp(t)
	app.configReader.SetDefault("database.name", ":memory:")

	app.WithDatabase(sqlite.Driver()).
		WithMessaging(broker).
		WithOutbox().
		InitServices()
	if err := app.Err(); err != nil {
		t.Fatalf("build: %v", err)
	}
	t.Cleanup(app.Close)

	// The WithOutbox Setup handler migrates the table once services are up.
	if err := app.runSetups(); err != nil {
		t.Fatalf("runSetups: %v", err)
	}
	if !app.DB().Migrator().HasTable("outbox_messages") {
		t.Fatal("outbox table was not migrated")
	}

	// Enqueue a message and drive the relay worker once.
	if err := outbox.EnqueueJSON(app.DB(), "orders.created", map[string]any{"id": 7}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	relay := findWorker(t, app, "outbox-relay")
	if err := relay(context.Background(), app); err != nil {
		t.Fatalf("relay: %v", err)
	}
	if len(broker.published) != 1 || broker.published[0].Subject != "orders.created" {
		t.Errorf("published = %+v, want one orders.created", broker.published)
	}
}

func TestWithOutboxRelayWithoutMessagingFails(t *testing.T) {
	app := newTestApp(t)
	app.configReader.SetDefault("database.name", ":memory:")
	app.WithDatabase(sqlite.Driver()).WithOutbox().InitServices()
	if err := app.Err(); err != nil {
		t.Fatalf("build: %v", err)
	}
	t.Cleanup(app.Close)
	if err := app.runSetups(); err != nil {
		t.Fatalf("runSetups: %v", err)
	}

	relay := findWorker(t, app, "outbox-relay")
	if err := relay(context.Background(), app); err == nil {
		t.Error("expected relay to fail without a messaging client")
	}
}
