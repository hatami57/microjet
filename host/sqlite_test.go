package host

import (
	"testing"

	"github.com/hatami57/microjet/gormx"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return app
}

func TestWithDatabaseSQLiteInMemory(t *testing.T) {
	app := newTestApp(t)
	app.configLoader.SetDefault("database.name", ":memory:")
	app.WithDatabase(gormx.SQLite()).InitServices()
	if err := app.Err(); err != nil {
		t.Fatalf("WithDatabase(SQLite()): %v", err)
	}
	if app.DB() == nil {
		t.Fatal("expected default DB to be registered")
	}
	t.Cleanup(app.Close)
}

func TestWithDatabaseFromConfigDispatchesSQLite(t *testing.T) {
	app := newTestApp(t)
	app.configLoader.SetDefault("database.driver", "sqlite")
	app.configLoader.SetDefault("database.name", ":memory:")
	app.WithDatabaseFromConfig().InitServices()
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
	app.configLoader.SetDefault("database.driver", "mongodb")
	app.WithDatabaseFromConfig().InitServices()
	if app.Err() == nil {
		t.Fatal("expected an error for unsupported driver")
	}
}

func TestWithDatabaseFromConfigRequiresDriver(t *testing.T) {
	app := newTestApp(t)
	// no driver default → empty string → error from connectDatabase
	app.WithDatabaseFromConfig().InitServices()
	if app.Err() == nil {
		t.Fatal("expected an error when no driver is configured")
	}
}
