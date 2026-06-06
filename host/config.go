package host

import (
	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/gormx"
	"github.com/hatami57/microjet/messaging"
)

// Config is the full application configuration loaded at startup.
type Config struct {
	App       *core.AppConfig           `mapstructure:"app"`
	Server    *core.ServerConfig        `mapstructure:"server"`
	Database  *gormx.Config            `mapstructure:"database"`
	Databases map[string]*gormx.Config `mapstructure:"databases"`
	Messaging *messaging.Config        `mapstructure:"messaging"`
	Log       *core.LogConfig          `mapstructure:"log"`
	Extra     any                       `mapstructure:"extra"`
}

// LoadConfig implements core.Configurable, loading all standard host sections.
func (c *Config) LoadConfig(l *core.ConfigLoader) error {
	if err := l.UnmarshalKey("app", &c.App); err != nil {
		return err
	}
	if err := l.UnmarshalKey("server", &c.Server); err != nil {
		return err
	}
	if err := l.UnmarshalKey("log", &c.Log); err != nil {
		return err
	}
	if err := l.UnmarshalKey("database", &c.Database); err != nil {
		return err
	}
	if err := l.UnmarshalKey("databases", &c.Databases); err != nil {
		return err
	}
	return l.UnmarshalKey("messaging", &c.Messaging)
}

// LoadConfig loads the full application configuration.
func LoadConfig(envPrefix string) (*Config, error) {
	return LoadConfigWithExtra[map[string]any](envPrefix)
}

// LoadConfigWithExtra is like LoadConfig but unmarshals the [extra] TOML section into T.
func LoadConfigWithExtra[T any](envPrefix string) (*Config, error) {
	cfg := &Config{}
	var extra T
	if err := core.LoadAll(envPrefix, cfg, core.ConfigurableFunc(func(l *core.ConfigLoader) error {
		return l.UnmarshalKey("extra", &extra)
	})); err != nil {
		return nil, err
	}
	cfg.Extra = extra
	return cfg, nil
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