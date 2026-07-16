package configx

import (
	"testing"
	"time"
)

type appSection struct {
	Name          string        `mapstructure:"name"`
	Debug         bool          `mapstructure:"debug"`
	ShutdownDelay time.Duration `mapstructure:"shutdownDelay"`
}

func TestMapReaderReadsSeededValues(t *testing.T) {
	r := NewMapReader(map[string]any{
		"app": map[string]any{"name": "billing", "debug": true, "shutdownDelay": "5s"},
	})

	var app appSection
	if err := r.Read("app", &app); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if app.Name != "billing" || !app.Debug {
		t.Errorf("Read app = %+v, want name=billing debug=true", app)
	}
	// String values decode to typed fields exactly as the file reader does.
	if app.ShutdownDelay != 5*time.Second {
		t.Errorf("ShutdownDelay = %v, want 5s", app.ShutdownDelay)
	}
}

func TestMapReaderSeededValueWinsOverDefault(t *testing.T) {
	r := NewMapReader(map[string]any{"app": map[string]any{"name": "billing"}})
	r.SetDefault("app.name", "fallback")

	var app appSection
	if err := r.Read("app", &app); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if app.Name != "billing" {
		t.Errorf("Name = %q, want seeded value to win over default", app.Name)
	}
}

func TestMapReaderDefaultAppliesWhenUnset(t *testing.T) {
	r := NewMapReader(nil)
	r.SetDefault("app.name", "fallback")

	var app appSection
	if err := r.Read("app", &app); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if app.Name != "fallback" {
		t.Errorf("Name = %q, want default to apply", app.Name)
	}
}

func TestMapReaderSetOverridesSeededValue(t *testing.T) {
	r := NewMapReader(map[string]any{"app": map[string]any{"name": "billing"}})
	setter, ok := r.(Setter)
	if !ok {
		t.Fatal("mapReader must implement Setter")
	}
	setter.Set("app.name", "override")

	var app appSection
	if err := r.Read("app", &app); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if app.Name != "override" {
		t.Errorf("Name = %q, want override to win", app.Name)
	}
}

func TestMapReaderReadAll(t *testing.T) {
	r := NewMapReader(map[string]any{"app": map[string]any{"name": "billing"}})

	var cfg struct {
		App appSection `mapstructure:"app"`
	}
	if err := r.ReadAll(&cfg); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if cfg.App.Name != "billing" {
		t.Errorf("ReadAll app.name = %q, want billing", cfg.App.Name)
	}
}
