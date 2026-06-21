package sqlite

import (
	"testing"

	"github.com/hatami57/microjet/gormx"
	"github.com/hatami57/microjet/host"
)

func TestModuleOpensInMemory(t *testing.T) {
	t.Setenv("APP_DATABASE_NAME", ":memory:")
	app, err := host.New()
	if err != nil {
		t.Fatalf("host.New: %v", err)
	}
	app.WithModule(gormx.Module(Driver())).InitServices()
	if err := app.Err(); err != nil {
		t.Fatalf("gormx.Module(sqlite.Driver()): %v", err)
	}
	if gormx.Of(app) == nil {
		t.Fatal("expected default DB to be registered")
	}
	t.Cleanup(app.Close)
}
