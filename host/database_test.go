package host

import (
	"testing"
)

func TestWithDatabasesFromConfigRegistersNamed(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	app.configLoader.SetDefault("database.primary.driver", "sqlite")
	app.configLoader.SetDefault("database.primary.name", ":memory:")
	app.configLoader.SetDefault("database.analytics.driver", "sqlite")
	app.configLoader.SetDefault("database.analytics.name", ":memory:")

	app.WithDatabasesFromConfig().InitServices()
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
	t.Cleanup(app.Close)
}

func TestWithDatabasesFromConfigReportsUnsupportedDriver(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	app.configLoader.SetDefault("database.bad.driver", "oracle")
	app.configLoader.SetDefault("database.bad.name", "test")
	app.WithDatabasesFromConfig().InitServices()
	if app.Err() == nil {
		t.Fatal("expected an error for an unsupported driver")
	}
}
