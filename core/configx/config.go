// Package configx loads layered configuration — TOML files, an optional local
// overlay, environment-variable overrides, and defaults — into typed structs.
package configx

import "github.com/hatami57/microjet/core/errorx"

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

// Validator is an optional interface a Configurable may implement to check its
// values once they have been read. When a Configurable also implements it,
// Configure and host.App call Validate immediately after ReadConfig and fail
// startup with the wrapped error — catching invalid settings (an empty DSN, an
// out-of-range port) at boot instead of at first use.
type Validator interface {
	Validate() error
}

// ReadAndValidate calls cfg.ReadConfig and, when cfg also implements Validator,
// its Validate method. It is the single seam that pairs reading with validation
// so every entry point (Configure, host.App) enforces Validate identically.
func ReadAndValidate(r Reader, cfg Configurable) error {
	if err := cfg.ReadConfig(r); err != nil {
		return err
	}
	if v, ok := cfg.(Validator); ok {
		if err := v.Validate(); err != nil {
			return errorx.NewInternalError("config", "config validation failed").WithInner(err)
		}
	}
	return nil
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
		if err := ReadAndValidate(reader, cfg); err != nil {
			return err
		}
	}

	return nil
}
