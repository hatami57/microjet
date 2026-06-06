package gormx

import "github.com/hatami57/microjet/core"

// Config is the database connection configuration, read from the [database]
// section of the application config (with APP_DATABASE_* env overrides).
type Config struct {
	Driver   string `mapstructure:"driver"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Name     string `mapstructure:"name"`
	SSLMode  string `mapstructure:"sslMode"`
}

// LoadConfig implements core.Configurable, loading the [database] section.
func (c *Config) LoadConfig(l *core.ConfigLoader) error {
	return l.UnmarshalKey("database", c)
}

// LoadConfigs loads all named database configs from the [databases] section.
func LoadConfigs(envPrefix string) (map[string]*Config, error) {
	var cfgs map[string]*Config
	if err := core.LoadAll(envPrefix, core.ConfigurableFunc(func(l *core.ConfigLoader) error {
		return l.UnmarshalKey("databases", &cfgs)
	})); err != nil {
		return nil, err
	}
	return cfgs, nil
}