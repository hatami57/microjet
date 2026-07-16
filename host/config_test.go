package host

import (
	"testing"
	"time"

	"github.com/hatami57/microjet/core/configx"
)

func TestLoadConfigAppliesDefaults(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if app.Config.App == nil {
		t.Fatal("expected App config to be populated from defaults")
	}
	if app.Config.App.Namespace != "App" {
		t.Errorf("App.Namespace = %q, want %q", app.Config.App.Namespace, "App")
	}
}

// serviceConfig is a Configurable used to prove Configure reads from the
// injected reader.
type serviceConfig struct {
	Endpoint string `mapstructure:"endpoint"`
}

func (s *serviceConfig) ReadConfig(r configx.Reader) error {
	r.SetDefault("service.endpoint", "unset")
	return r.Read("service", s)
}

func TestWithConfigReaderInjectsConfig(t *testing.T) {
	// Chdir to an empty temp dir: if the App fell back to file discovery this
	// would find no config.toml, but more importantly the injected reader must be
	// what populates Config — no filesystem discovery happens at all.
	t.Chdir(t.TempDir())

	reader := configx.NewMapReader(map[string]any{
		"app": map[string]any{"name": "billing", "shutdownDelay": "5s"},
	})
	app, err := New(WithConfigReader(reader))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if app.Config.App.Name != "billing" {
		t.Errorf("App.Name = %q, want injected %q", app.Config.App.Name, "billing")
	}
	if app.Config.App.ShutdownDelay != 5*time.Second {
		t.Errorf("App.ShutdownDelay = %v, want 5s", app.Config.App.ShutdownDelay)
	}
}

func TestConfigureUsesInjectedReader(t *testing.T) {
	t.Chdir(t.TempDir())

	reader := configx.NewMapReader(map[string]any{
		"service": map[string]any{"endpoint": "https://billing.internal"},
	})
	svc := &serviceConfig{}
	app := MustNew(WithConfigReader(reader)).Configure(svc)
	if err := app.Err(); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if svc.Endpoint != "https://billing.internal" {
		t.Errorf("service endpoint = %q, want injected value", svc.Endpoint)
	}
}

func TestWithConfigValueOverridesReader(t *testing.T) {
	t.Chdir(t.TempDir())

	reader := configx.NewMapReader(map[string]any{
		"app": map[string]any{"name": "seeded"},
	})
	app, err := New(
		WithConfigReader(reader),
		WithConfigValue("app.name", "overridden"),
		WithConfigValues(map[string]any{"app.debug": true}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if app.Config.App.Name != "overridden" {
		t.Errorf("App.Name = %q, want programmatic override to win", app.Config.App.Name)
	}
	if !app.Config.App.Debug {
		t.Error("App.Debug = false, want programmatic value true")
	}
}

func TestWithConfigValueOverridesFileReader(t *testing.T) {
	// With the default file reader (no config file present), a programmatic value
	// still applies — proving WithConfigValue is not tied to WithConfigReader.
	t.Chdir(t.TempDir())

	app, err := New(WithConfigValue("app.name", "from-code"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if app.Config.App.Name != "from-code" {
		t.Errorf("App.Name = %q, want %q", app.Config.App.Name, "from-code")
	}
}

// readerWithoutSetter implements configx.Reader but not configx.Setter, so
// WithConfigValue cannot layer onto it.
type readerWithoutSetter struct{}

func (readerWithoutSetter) SetDefault(string, any)        {}
func (readerWithoutSetter) Read(string, any) error        { return nil }
func (readerWithoutSetter) ReadMap(string) map[string]any { return nil }
func (readerWithoutSetter) ReadAll(any) error             { return nil }

func TestWithConfigValueErrorsWhenReaderUnsupported(t *testing.T) {
	_, err := New(
		WithConfigReader(readerWithoutSetter{}),
		WithConfigValue("app.name", "x"),
	)
	if err == nil {
		t.Fatal("expected error when reader does not support programmatic values")
	}
}

func TestEnvironmentHelpers(t *testing.T) {
	cases := []struct {
		env         string
		isProd      bool
		isDev       bool
		isTest      bool
		environment string
	}{
		{"production", true, false, false, EnvProduction},
		{"prod", true, false, false, EnvProduction},
		{"development", false, true, false, EnvDevelopment},
		{"dev", false, true, false, EnvDevelopment},
		{"test", false, false, true, EnvTest},
	}
	for _, c := range cases {
		a := &AppConfig{Environment: c.env}
		if a.IsProduction() != c.isProd || a.IsDevelopment() != c.isDev || a.IsTest() != c.isTest {
			t.Errorf("%q: prod=%v dev=%v test=%v", c.env, a.IsProduction(), a.IsDevelopment(), a.IsTest())
		}
		if a.GetEnvironment() != c.environment {
			t.Errorf("%q: GetEnvironment = %q, want %q", c.env, a.GetEnvironment(), c.environment)
		}
	}
}
