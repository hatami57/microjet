// Package otelx wires OpenTelemetry tracing into a MicroJet application. It
// owns the global tracer provider and W3C propagator; the other MicroJet
// modules (httpx, gormx, messaging/nats) instrument themselves through the
// otel globals, so they trace automatically once a Tracing service is
// registered — and stay zero-overhead no-ops when it is not.
package otelx

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/core/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

var (
	_ config.Configurable = (*Tracing)(nil)
	_ core.Initer         = (*Tracing)(nil)
	_ core.Closer         = (*Tracing)(nil)
)

// shutdownTimeout bounds how long Close waits for the final span flush so a
// slow or unreachable collector cannot stall application shutdown.
const shutdownTimeout = 5 * time.Second

// Tracing is the lifecycle service that configures the global OpenTelemetry
// tracer provider. Hand it to host.WithTracing (or register it yourself with
// ProvideService); the host loads its config, installs the provider during
// init, and flushes pending spans on shutdown.
type Tracing struct {
	Config Config

	logger         *slog.Logger
	provider       *sdktrace.TracerProvider
	serviceName    string
	serviceVersion string
}

// New returns a Tracing service ready to be registered with the host.
func New() *Tracing {
	return &Tracing{logger: slog.Default()}
}

// SetLogger sets the logger used for setup messages and async export errors.
// The host calls it during WithTracing; a nil logger is ignored.
func (t *Tracing) SetLogger(logger *slog.Logger) {
	if logger != nil {
		t.logger = logger
	}
}

// SetServiceInfo sets the fallback service name and version used when the
// [tracing] section does not specify them. The host passes [app] name/version.
func (t *Tracing) SetServiceInfo(name, version string) {
	t.serviceName = name
	t.serviceVersion = version
}

// ReadConfig implements config.Configurable, reading the [tracing] section.
func (t *Tracing) ReadConfig(l config.Reader) error {
	l.SetDefault("tracing.enabled", true)
	l.SetDefault("tracing.endpoint", "localhost:4318")
	l.SetDefault("tracing.insecure", true)
	l.SetDefault("tracing.sampleRatio", 1.0)
	return l.Read("tracing", &t.Config)
}

// Init implements core.Initer. It builds the OTLP/HTTP exporter and tracer
// provider and installs them globally, together with the W3C trace-context +
// baggage propagator. A disabled config leaves the no-op globals untouched.
// The exporter does not dial the collector here; an unreachable endpoint
// surfaces as logged export warnings, never as a startup failure.
func (t *Tracing) Init() error {
	if !t.Config.Enabled {
		t.logger.Debug("tracing disabled")
		return nil
	}

	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(t.Config.Endpoint)}
	if t.Config.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	exporter, err := otlptracehttp.New(context.Background(), opts...)
	if err != nil {
		return fmt.Errorf("tracing: creating OTLP exporter: %w", err)
	}

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(t.resolvedServiceName()),
		semconv.ServiceVersion(t.resolvedServiceVersion()),
	))
	if err != nil {
		return fmt.Errorf("tracing: building resource: %w", err)
	}

	t.provider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(t.Config.SampleRatio))),
	)
	otel.SetTracerProvider(t.provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))
	// Export failures are operational noise (collector down, network blip), not
	// application errors; route them to the service logger at warn level.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		t.logger.Warn("otel export error", "error", err)
	}))

	t.logger.Info("tracing enabled",
		"endpoint", t.Config.Endpoint,
		"service", t.resolvedServiceName(),
		"sampleRatio", t.Config.SampleRatio,
	)
	return nil
}

// Close implements core.Closer, flushing pending spans and shutting the
// provider down. Bounded by shutdownTimeout so a dead collector cannot hang
// process exit.
func (t *Tracing) Close() error {
	if t.provider == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return t.provider.Shutdown(ctx)
}

// Provider returns the installed tracer provider, or nil when tracing is
// disabled or Init has not run. Useful for tests and manual flushing.
func (t *Tracing) Provider() *sdktrace.TracerProvider { return t.provider }

func (t *Tracing) resolvedServiceName() string {
	if t.Config.ServiceName != "" {
		return t.Config.ServiceName
	}
	return t.serviceName
}

func (t *Tracing) resolvedServiceVersion() string {
	if t.Config.ServiceVersion != "" {
		return t.Config.ServiceVersion
	}
	return t.serviceVersion
}
