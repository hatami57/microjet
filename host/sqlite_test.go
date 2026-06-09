package host

import (
	"testing"

	"github.com/hatami57/microjet/gormx/sqlite"
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
	app.configReader.SetDefault("database.name", ":memory:")
	app.WithDatabase(sqlite.Driver()).InitServices()
	if err := app.Err(); err != nil {
		t.Fatalf("WithDatabase(SQLite()): %v", err)
	}
	if app.DB() == nil {
		t.Fatal("expected default DB to be registered")
	}
	t.Cleanup(app.Close)
}
