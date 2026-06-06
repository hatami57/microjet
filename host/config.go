package host

import (
	"strings"

	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/gormx"
)

const (
	EnvTest        = "test"
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

type AppConfig struct {
	Namespace   string `mapstructure:"namespace"`
	Environment string `mapstructure:"environment"`
	Name        string `mapstructure:"name"`
	Version     string `mapstructure:"version"`
	Debug       bool   `mapstructure:"debug"`
}

func (a *AppConfig) GetEnvironment() string {
	switch {
	case a.IsTest():
		return EnvTest
	case a.IsDevelopment():
		return EnvDevelopment
	default:
		return EnvProduction
	}
}

func (a *AppConfig) GetName() string    { return a.Name }
func (a *AppConfig) GetVersion() string { return a.Version }
func (a *AppConfig) GetDebug() bool     { return a.Debug }

func (a *AppConfig) IsProduction() bool {
	env := strings.ToLower(a.Environment)
	return env == "production" || env == "prod"
}

func (a *AppConfig) IsDevelopment() bool {
	env := strings.ToLower(a.Environment)
	return env == "development" || env == "dev"
}

func (a *AppConfig) IsTest() bool {
	return strings.ToLower(a.Environment) == "test"
}

type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// Config is the full application configuration loaded at startup.
type Config struct {
	App       *AppConfig               `mapstructure:"app"`
	Server    *ServerConfig            `mapstructure:"server"`
	Database  *gormx.Config            `mapstructure:"database"`
	Databases map[string]*gormx.Config `mapstructure:"databases"`
	Log       *core.LogConfig          `mapstructure:"log"`
}

// LoadConfig implements core.Configurable, loading all standard host sections.
// It sets app and server defaults before unmarshaling so they apply when no
// config file is present.
func (c *Config) LoadConfig(l *core.ConfigLoader) error {
	l.SetDefault("app.namespace", "App")
	l.SetDefault("app.environment", "development")
	l.SetDefault("app.name", "App")
	l.SetDefault("app.version", "0.1.0")
	// Default to non-debug: debug mode enables verbose logging, Swagger, and
	// inner-error exposure in HTTP responses — none of which are safe defaults
	// for a library that may be embedded in production. Opt in via app.debug=true.
	l.SetDefault("app.debug", false)
	l.SetDefault("server.host", "localhost")
	l.SetDefault("server.port", 8080)

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
	return l.UnmarshalKey("databases", &c.Databases)
}

// LoadConfig loads the standard host configuration sections as a standalone call.
func LoadConfig(envPrefix string) (*Config, error) {
	loader, err := core.NewConfigLoader(envPrefix)
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	if err := loader.Configure(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}