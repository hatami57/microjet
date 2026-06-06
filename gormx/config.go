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

// LoadConfig loads the [database] config section using the shared viper setup.
func LoadConfig(envPrefix string) (*Config, error) {
	return core.LoadSection[Config]("database", envPrefix)
}

// LoadConfigs loads all named database configs from the [databases] section.
func LoadConfigs(envPrefix string) (map[string]*Config, error) {
	m, err := core.LoadSection[map[string]*Config]("databases", envPrefix)
	if err != nil {
		return nil, err
	}
	return *m, nil
}