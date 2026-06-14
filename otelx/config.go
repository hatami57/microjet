package otelx

// Config controls how tracing is set up, read from the [tracing] config
// section. The zero value plus ReadConfig defaults gives a tracer that exports
// to a local OTLP/HTTP collector (localhost:4318) sampling every trace.
type Config struct {
	// Enabled turns span export on or off. When false, Init is a no-op and the
	// process keeps the default no-op tracer provider.
	Enabled bool `mapstructure:"enabled"`
	// Endpoint is the host:port of the OTLP/HTTP collector (no scheme).
	Endpoint string `mapstructure:"endpoint"`
	// Insecure sends spans over plain HTTP instead of TLS. Local collectors
	// usually listen without TLS, so this defaults to true; set it to false and
	// point Endpoint at your gateway in production.
	Insecure bool `mapstructure:"insecure"`
	// SampleRatio is the fraction of root traces to sample in [0, 1]. Child
	// spans always follow their parent's sampling decision.
	SampleRatio float64 `mapstructure:"sampleRatio"`
	// ServiceName overrides the service.name resource attribute. When empty,
	// the host fills it from [app] name.
	ServiceName string `mapstructure:"serviceName"`
	// ServiceVersion overrides the service.version resource attribute. When
	// empty, the host fills it from [app] version.
	ServiceVersion string `mapstructure:"serviceVersion"`
}
