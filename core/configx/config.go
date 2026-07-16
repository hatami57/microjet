// Package configx loads layered configuration — TOML files, an optional local
// overlay, environment-variable overrides, and defaults — into typed structs.
package configx

// Reader wraps a reader instance and exposes config-reading operations to
// Configurable implementations.
type Reader interface {
	SetDefault(key string, value any)
	Read(key string, dest any) error
	ReadMap(key string) map[string]any
	ReadAll(dest any) error
}

// Setter is an optional interface a Reader may implement to accept programmatic
// override values that win over its other layers — config files, environment
// variables, and defaults. Both the Viper file reader (NewViperConfigReader)
// and the map reader (NewMapReader) implement it; host.WithConfigValue relies
// on it to inject settings in code.
type Setter interface {
	Set(key string, value any)
}

// Configurable is implemented by any type that can populate itself from a
// Reader. ReadConfig is called on each registered value in order.
type Configurable interface {
	ReadConfig(Reader) error
}

// Configure creates a single Reader and calls ReadConfig on each
// Configurable in order. Use NewViperConfigReader when you need to reuse the
// same parsed config across multiple calls.
func Configure(envPrefix string, cfgs ...Configurable) error {
	reader, err := NewViperConfigReader(envPrefix)
	if err != nil {
		return err
	}

	for _, cfg := range cfgs {
		if err := cfg.ReadConfig(reader); err != nil {
			return err
		}
	}

	return nil
}
