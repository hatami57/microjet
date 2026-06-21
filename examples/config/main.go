// Command config demonstrates MicroJet's configuration system: TOML files,
// environment-variable overrides, a local overlay file, sane defaults, and
// generic typed access to your own config sections.
//
// The host reads the standard [app] and [log] sections for you. Your feature
// modules add their own sections by implementing configx.Configurable and
// passing themselves to app.Configure — the file is parsed once and shared.
//
// Try it:
//
//	go run .                              # values from config.toml
//	APP_PAYMENTS_CURRENCY=EUR go run .    # env override wins (APP_<SECTION>_<KEY>)
//	APP_APP_NAME=svc-2 go run .           # env override of a host section
//
// Env vars override the file, which overrides the SetDefault values. For
// environment-specific overrides kept in a file instead, create an uncommitted
// config.local.toml — it is merged on top of config.toml.
//
// config.toml is the committed baseline; config.local.toml is an optional,
// uncommitted overlay merged on top — the idiomatic way to override values per
// machine/environment without editing the checked-in file.
package main

import (
	"github.com/hatami57/microjet/core/configx"
	"github.com/hatami57/microjet/host"
)

// PaymentsConfig is a feature-owned config section. Implementing
// configx.Configurable lets the host load it through the shared reader.
type PaymentsConfig struct {
	Currency   string `mapstructure:"currency"`
	MaxRetries int    `mapstructure:"maxRetries"`
	Sandbox    bool   `mapstructure:"sandbox"`
}

// ReadConfig sets defaults (so the section is optional) then unmarshals the
// [payments] table over them. SetDefault values apply when the key is absent
// from both the file and the environment.
func (c *PaymentsConfig) ReadConfig(l configx.Reader) error {
	l.SetDefault("payments.currency", "USD")
	l.SetDefault("payments.maxRetries", 3)
	l.SetDefault("payments.sandbox", true)
	return l.Read("payments", c)
}

func main() {
	// MustNew already parsed the file and loaded [app]/[log]. Configure layers in
	// our own section using the same reader (no second file read).
	app := host.MustNew()
	defer app.Close()

	payments := &PaymentsConfig{}
	app.Configure(payments)
	if err := app.Err(); err != nil {
		panic(err)
	}

	// Standard host config, read for you by MustNew.
	app.Logger.Info("host config (from [app])",
		"name", app.Config.App.Name,
		"environment", app.Config.App.GetEnvironment(),
		"debug", app.Config.App.Debug,
	)

	// Our feature config — env vars (APP_PAYMENTS_*) override config.toml, which
	// in turn overrides the SetDefault values.
	app.Logger.Info("feature config (from [payments])",
		"currency", payments.Currency,
		"maxRetries", payments.MaxRetries,
		"sandbox", payments.Sandbox,
	)
}
