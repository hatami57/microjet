package core

import "testing"

func TestLoadDoesNotErrorWithNoFile(t *testing.T) {
	// Running from the package dir there is no config.toml — Load must succeed
	// using only defaults or zero values without returning an error.
	var cfg struct {
		Foo string `mapstructure:"foo"`
	}
	if err := Load(&cfg, ""); err != nil {
		t.Fatalf("Load returned error with no config file: %v", err)
	}
}