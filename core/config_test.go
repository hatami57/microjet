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

func TestGetExtraTypedConversion(t *testing.T) {
	cfg := &Config{Extra: map[string]any{
		"workers": "8",
		"ratio":   "1.5",
		"enabled": "true",
	}}

	workers, err := GetExtra[int](cfg, "workers")
	if err != nil || workers != 8 {
		t.Errorf("GetExtra[int] = %d, %v; want 8, nil", workers, err)
	}
	ratio, err := GetExtra[float64](cfg, "ratio")
	if err != nil || ratio != 1.5 {
		t.Errorf("GetExtra[float64] = %v, %v; want 1.5, nil", ratio, err)
	}
	enabled, err := GetExtra[bool](cfg, "enabled")
	if err != nil || !enabled {
		t.Errorf("GetExtra[bool] = %v, %v; want true, nil", enabled, err)
	}
	if _, err := GetExtra[int](cfg, "missing"); err == nil {
		t.Error("expected error for missing key")
	}
}
