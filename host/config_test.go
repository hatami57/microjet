package host

import "testing"

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
	if app.Config.Server == nil || app.Config.Server.Port != 8080 {
		t.Errorf("Server.Port = %v, want 8080", app.Config.Server)
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
