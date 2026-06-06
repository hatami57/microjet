package core

import "testing"

func TestLoadUsesDefaultsWhenNoFile(t *testing.T) {
	// Running from the package dir, there is no config.toml — Load must fall
	// back to defaults instead of returning an error.
	cfg := &Config{}
	if err := Load(cfg, ""); err != nil {
		t.Fatalf("Load returned error with no config file: %v", err)
	}
	if cfg.App == nil {
		t.Fatal("expected App config to be populated from defaults")
	}
	if cfg.App.Namespace != "App" {
		t.Errorf("App.Namespace = %q, want %q", cfg.App.Namespace, "App")
	}
	if cfg.Server == nil || cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %v, want 8080", cfg.Server)
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

func TestGetExtraConfig(t *testing.T) {
	type myExtra struct {
		Workers int
		Queue   string
	}

	want := myExtra{Workers: 4, Queue: "jobs"}
	cfg := &Config{Extra: want}

	got, ok := GetExtraConfig[myExtra](cfg)
	if !ok || got != want {
		t.Errorf("GetExtraConfig = %v, %v; want %v, true", got, ok, want)
	}

	_, ok = GetExtraConfig[string](cfg)
	if ok {
		t.Error("GetExtraConfig[string] should return false for wrong type")
	}
}

func TestMustGetExtraConfigPanicsOnWrongType(t *testing.T) {
	cfg := &Config{Extra: 42}
	defer func() {
		if recover() == nil {
			t.Error("expected panic for wrong extra type")
		}
	}()
	MustGetExtraConfig[string](cfg)
}
