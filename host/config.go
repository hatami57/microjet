package host

import (
	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/gormx"
	"github.com/hatami57/microjet/messaging"
)

// Config is the full application configuration loaded at startup.
type Config struct {
	App       *core.AppConfig              `mapstructure:"app"`
	Server    *core.ServerConfig           `mapstructure:"server"`
	Database  *gormx.Config               `mapstructure:"database"`
	Databases map[string]*gormx.Config    `mapstructure:"databases"`
	Messaging *messaging.Config           `mapstructure:"messaging"`
	Log       *core.LogConfig             `mapstructure:"log"`
	Extra     any                          `mapstructure:"extra"`
}

// LoadConfig loads the full application configuration.
func LoadConfig(envPrefix string) (*Config, error) {
	return LoadConfigWithExtra[map[string]any](envPrefix)
}

// LoadConfigWithExtra is like LoadConfig but unmarshals the [extra] TOML section into T.
func LoadConfigWithExtra[T any](envPrefix string) (*Config, error) {
	type raw struct {
		App       *core.AppConfig              `mapstructure:"app"`
		Server    *core.ServerConfig           `mapstructure:"server"`
		Database  *gormx.Config               `mapstructure:"database"`
		Databases map[string]*gormx.Config    `mapstructure:"databases"`
		Messaging *messaging.Config           `mapstructure:"messaging"`
		Log       *core.LogConfig             `mapstructure:"log"`
		Extra     T                            `mapstructure:"extra"`
	}
	r := &raw{}
	if err := core.Load(r, envPrefix); err != nil {
		return nil, err
	}
	return &Config{
		App:       r.App,
		Server:    r.Server,
		Database:  r.Database,
		Databases: r.Databases,
		Messaging: r.Messaging,
		Log:       r.Log,
		Extra:     r.Extra,
	}, nil
}

// GetExtraConfig casts Config.Extra to T, returning false if the cast fails.
func GetExtraConfig[T any](c *Config) (T, bool) {
	typed, ok := c.Extra.(T)
	return typed, ok
}

// MustGetExtraConfig casts Config.Extra to T, panicking if the cast fails.
func MustGetExtraConfig[T any](c *Config) T {
	typed, ok := c.Extra.(T)
	if !ok {
		panic("config extra type mismatch")
	}
	return typed
}