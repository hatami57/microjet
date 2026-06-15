package otelx

import (
	"testing"

	"github.com/hatami57/microjet/core/config"
	"go.opentelemetry.io/otel"
)

func TestReadConfigDefaults(t *testing.T) {
	reader, err := config.NewViperConfigReader("OTELXTEST")
	if err != nil {
		t.Fatalf("NewViperConfigReader: %v", err)
	}
	tr := New()
	if err := tr.ReadConfig(reader); err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if !tr.Config.Enabled {
		t.Error("expected enabled by default")
	}
	if tr.Config.Endpoint != "localhost:4318" {
		t.Errorf("endpoint = %q", tr.Config.Endpoint)
	}
	if !tr.Config.Insecure {
		t.Error("expected insecure by default")
	}
	if tr.Config.SampleRatio != 1.0 {
		t.Errorf("sampleRatio = %v", tr.Config.SampleRatio)
	}
}

func TestInitDisabledIsNoop(t *testing.T) {
	before := otel.GetTracerProvider()
	tr := New()
	tr.Config = Config{Enabled: false}
	if err := tr.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if tr.Provider() != nil {
		t.Error("disabled tracing must not build a provider")
	}
	if otel.GetTracerProvider() != before {
		t.Error("disabled tracing must not replace the global provider")
	}
	if err := tr.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestInitInstallsGlobalProvider(t *testing.T) {
	before := otel.GetTracerProvider()
	defer otel.SetTracerProvider(before)

	tr := New()
	tr.SetServiceInfo("svc", "1.2.3")
	tr.Config = Config{Enabled: true, Endpoint: "localhost:4318", Insecure: true, SampleRatio: 0.5}
	if err := tr.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() {
		if err := tr.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	if tr.Provider() == nil {
		t.Fatal("expected a provider")
	}
	if otel.GetTracerProvider() != tr.Provider() {
		t.Error("global provider not installed")
	}
}

func TestConfigOverridesServiceInfo(t *testing.T) {
	tr := New()
	tr.SetServiceInfo("from-app", "0.1.0")
	tr.Config.ServiceName = "from-config"
	if got := tr.resolvedServiceName(); got != "from-config" {
		t.Errorf("resolvedServiceName = %q", got)
	}
	if got := tr.resolvedServiceVersion(); got != "0.1.0" {
		t.Errorf("resolvedServiceVersion = %q", got)
	}
}
