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

// A section present in the seeded map must still take defaults for the keys it
// does not mention. Viper resolves Get("app") from the first layer holding the
// key, so without promotion the config layer's partial map shadows every
// default registered under that section.
func TestMapReaderDefaultsFillGapsInSeededSection(t *testing.T) {
	r := NewMapReader(map[string]any{"app": map[string]any{"name": "billing"}})
	r.SetDefault("app.debug", true)
	r.SetDefault("app.shutdownDelay", "5s")

	var app appSection
	if err := r.Read("app", &app); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if app.Name != "billing" {
		t.Errorf("Name = %q, want the seeded value", app.Name)
	}
	if !app.Debug {
		t.Error("Debug = false, want the default to fill the gap the seeded map left")
	}
	if app.ShutdownDelay != 5*time.Second {
		t.Errorf("ShutdownDelay = %v, want the default 5s", app.ShutdownDelay)
	}
}

func TestMapReaderReadAllDefaultsFillGapsInSeededSection(t *testing.T) {
	r := NewMapReader(map[string]any{"app": map[string]any{"name": "billing"}})
	r.SetDefault("app.debug", true)

	var cfg struct {
		App appSection `mapstructure:"app"`
	}
	if err := r.ReadAll(&cfg); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if cfg.App.Name != "billing" {
		t.Errorf("app.name = %q, want billing", cfg.App.Name)
	}
	if !cfg.App.Debug {
		t.Error("app.debug = false, want the default to fill the gap")
	}
}

func TestMapReaderReadMapDefaultsFillGapsInSeededSection(t *testing.T) {
	r := NewMapReader(map[string]any{"app": map[string]any{"name": "billing"}})
	r.SetDefault("app.debug", true)

	got := r.ReadMap("app")
	if got["name"] != "billing" {
		t.Errorf("name = %v, want billing", got["name"])
	}
	if got["debug"] != true {
		t.Errorf("debug = %v, want the default true", got["debug"])
	}
}
