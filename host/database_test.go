package host

import (
	"testing"

	"github.com/hatami57/microjet/gormx"
)

func TestWithDatabasesFromConfigRegistersNamed(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	app.Config.Databases = map[string]*gormx.Config{
		"primary":   {Driver: "sqlite", Name: ":memory:"},
		"analytics": {Driver: "sqlite", Name: ":memory:"},
	}

	app.WithDatabasesFromConfig()
	if err := app.Err(); err != nil {
		t.Fatalf("WithDatabasesFromConfig: %v", err)
	}

	for _, name := range []string{"primary", "analytics"} {
		if app.NamedDB(name) == nil {
			t.Errorf("named database %q not registered", name)
		}
	}
	if app.NamedDB("missing") != nil {
		t.Error("unexpected database for unknown name")
	}
}

func TestWithDatabasesFromConfigReportsUnsupportedDriver(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	app.Config.Databases = map[string]*gormx.Config{
		"bad": {Driver: "oracle"},
	}
	app.WithDatabasesFromConfig()
	if app.Err() == nil {
		t.Fatal("expected an error for an unsupported driver")
	}
}
