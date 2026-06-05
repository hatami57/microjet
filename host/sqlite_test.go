package host

import (
	"testing"

	"github.com/hatami57/microjet/core"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	app.Config.Database = &core.DatabaseConfig{}
	return app
}

func TestWithSQLiteInMemory(t *testing.T) {
	app := newTestApp(t)
	app.Config.Database.Name = ":memory:"

	app.WithSQLite()

	if err := app.Err(); err != nil {
		t.Fatalf("WithSQLite: %v", err)
	}
	if app.DB() == nil {
		t.Fatal("expected default DB to be registered")
	}
	t.Cleanup(app.Close)
}

func TestWithDatabaseFromConfigDispatchesSQLite(t *testing.T) {
	app := newTestApp(t)
	app.Config.Database.Driver = "sqlite"
	app.Config.Database.Name = ":memory:"

	app.WithDatabaseFromConfig()

	if err := app.Err(); err != nil {
		t.Fatalf("WithDatabaseFromConfig: %v", err)
	}
	if app.DB() == nil {
		t.Fatal("expected default DB to be registered")
	}
	t.Cleanup(app.Close)
}

func TestWithDatabaseFromConfigRejectsUnknownDriver(t *testing.T) {
	app := newTestApp(t)
	app.Config.Database.Driver = "mongodb"

	app.WithDatabaseFromConfig()

	if app.Err() == nil {
		t.Fatal("expected an error for unsupported driver")
	}
}

func TestWithDatabaseFromConfigRequiresDriver(t *testing.T) {
	app := newTestApp(t)
	app.Config.Database.Driver = ""

	app.WithDatabaseFromConfig()

	if app.Err() == nil {
		t.Fatal("expected an error when no driver is configured")
	}
}
