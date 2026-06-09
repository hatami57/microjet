package core

// ConfigReader wraps a reader instance and exposes config-reading operations to
// Configurable implementations.
type ConfigReader interface {
	SetDefault(key string, value any)
	Read(key string, dest any) error
	ReadAsMap(key string) map[string]any
	ReadAll(dest any) error
}

// Configurable is implemented by any type that can populate itself from a
// ConfigReader. ReadConfig is called on each registered value in order.
type Configurable interface {
	ReadConfig(ConfigReader) error
}

// Configure creates a single ConfigReader and calls ReadConfig on each
// Configurable in order. Use NewConfigReader when you need to reuse the
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
