// Command config-validation demonstrates the configx validation hook: when a
// config struct implements Validate() error, it is checked immediately after it
// is read, so invalid settings fail at startup with a clear, wrapped error
// instead of surfacing much later at first use (an empty DSN discovered at the
// first query, a bad port discovered when the listener binds).
//
// The host calls this automatically — host.New reads and validates its own
// config, and App.Configure validates every struct you pass it. Here we drive
// the same seam directly, configx.ReadAndValidate, against in-memory config so
// the program runs offline:
//
//	go run .
package main

import (
	"errors"
	"fmt"

	"github.com/hatami57/microjet/core/configx"
	"github.com/hatami57/microjet/core/errorx"
)

// PaymentsConfig reads the [payments] section and validates its own values.
// Implementing configx.Validator (Validate() error) is all it takes to opt in.
type PaymentsConfig struct {
	Currency   string `mapstructure:"currency"`
	MaxRetries int    `mapstructure:"maxRetries"`
}

func (c *PaymentsConfig) ReadConfig(r configx.Reader) error {
	return r.Read("payments", c)
}

// Validate runs right after ReadConfig. Return any error; ReadAndValidate wraps
// it as an errorx internal error so it fails the boot with a clear message.
func (c *PaymentsConfig) Validate() error {
	if c.Currency == "" {
		return errors.New("currency must be set")
	}
	if c.MaxRetries < 0 || c.MaxRetries > 10 {
		return fmt.Errorf("maxRetries must be between 0 and 10, got %d", c.MaxRetries)
	}
	return nil
}

func main() {
	// A valid config reads and validates cleanly.
	good := configx.NewMapReader(map[string]any{
		"payments": map[string]any{"currency": "USD", "maxRetries": 3},
	})
	var cfg PaymentsConfig
	if err := configx.ReadAndValidate(good, &cfg); err != nil {
		panic(err)
	}
	fmt.Printf("valid config accepted:   %+v\n\n", cfg)

	// An invalid config is rejected at load time — before anything uses it.
	bad := configx.NewMapReader(map[string]any{
		"payments": map[string]any{"currency": "", "maxRetries": 99},
	})
	err := configx.ReadAndValidate(bad, &PaymentsConfig{})
	fmt.Println("invalid config rejected:")
	fmt.Printf("  error:            %v\n", err)
	fmt.Printf("  errorx internal:  %v\n", errors.Is(err, errorx.ErrInternal))

	// The wrapped cause is preserved, so callers can still inspect it.
	var ex *errorx.Error
	if errors.As(err, &ex) {
		fmt.Printf("  wrapped cause:    %v\n", ex.Inner)
	}
}
