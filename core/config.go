package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
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

type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// LogConfig configures the logger. Console output is always enabled unless
// explicitly disabled via Console.Enabled=false. A file output is added when
// File.Enabled=true and File.Path is set. Each output can independently
// override the top-level Level and Format.
// Valid levels: debug, info, warn, error. Valid formats: text, json.
type LogConfig struct {
	Level   string           `mapstructure:"level"`
	Format  string           `mapstructure:"format"`
	Console *LogOutputConfig `mapstructure:"console"`
	File    *LogOutputConfig `mapstructure:"file"`
}

// LogOutputConfig configures a single log output destination (console or file).
type LogOutputConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Level   string `mapstructure:"level"`  // overrides LogConfig.Level for this output
	Format  string `mapstructure:"format"` // overrides LogConfig.Format for this output
	Path    string `mapstructure:"path"`   // file output only; parent dirs are created automatically
}

// ConfigLoader wraps a viper instance and exposes config-loading operations to
// Configurable implementations without leaking the viper dependency.
type ConfigLoader struct {
	v *viper.Viper
}

// UnmarshalKey unmarshals the named config section into dest.
func (l *ConfigLoader) UnmarshalKey(section string, dest any) error {
	return l.v.UnmarshalKey(section, dest)
}

// Configurable is implemented by any type that can populate itself from a
// ConfigLoader. LoadAll calls LoadConfig on each registered value in order,
// passing the same parsed viper instance to all of them.
type Configurable interface {
	LoadConfig(*ConfigLoader) error
}

// PostConfigLoader is an optional extension of Configurable. If a Configurable
// also implements PostConfigLoader, LoadAll calls PostLoadConfig immediately
// after LoadConfig succeeds, allowing validation or derived-field initialization.
type PostConfigLoader interface {
	PostLoadConfig() error
}

// ConfigurableFunc is a function adapter for Configurable, analogous to http.HandlerFunc.
type ConfigurableFunc func(*ConfigLoader) error

func (f ConfigurableFunc) LoadConfig(l *ConfigLoader) error { return f(l) }

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

// NewViper builds the viper instance microjet uses to load configuration:
// it searches the standard config paths, reads config.toml plus an optional
// config.local.toml overlay, and binds APP_* environment overrides. It is
// exported so provider-specific modules (e.g. aws) can load their own typed
// config sections without core having to depend on them.
// It does not set any app-level defaults; call SetAppDefaults or LoadAll to get those.
func NewViper(envPrefix string) (*viper.Viper, error) {
	v := viper.New()
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	exePath, err := os.Executable()
	if err != nil {
		return nil, err
	}
	exeDir := filepath.Dir(exePath)
	dirs := []string{cwd, filepath.Join(cwd, "config"), exeDir, filepath.Join(exeDir, "config")}
	for _, dir := range dirs {
		v.AddConfigPath(dir)
	}
	v.SetConfigType("toml")

	if envPrefix == "" {
		envPrefix = "APP"
	}
	v.SetEnvPrefix(strings.ToUpper(envPrefix))
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.SetConfigName("config")
	if err := v.ReadInConfig(); err != nil {
		// A missing config file is not fatal: defaults + env vars are enough to
		// boot. Any other read error (malformed TOML, permissions) is fatal.
		if _, ok := errors.AsType[viper.ConfigFileNotFoundError](err); !ok {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
	}

	v.SetConfigName("config.local")
	_ = v.MergeInConfig() // optional overlay; absence is fine

	return v, nil
}

// SetAppDefaults sets the core app/server defaults on v. Called by LoadAll and
// Load so that app/server fields are never zero when no config file is present.
func SetAppDefaults(v *viper.Viper) {
	v.SetDefault("app.namespace", "App")
	v.SetDefault("app.environment", "development")
	v.SetDefault("app.name", "App")
	v.SetDefault("app.version", "0.1.0")
	// Default to non-debug: debug mode enables verbose logging, Swagger, and
	// inner-error exposure in HTTP responses — none of which are safe defaults
	// for a library that may be embedded in production. Opt in via app.debug=true.
	v.SetDefault("app.debug", false)
	v.SetDefault("server.host", "localhost")
	v.SetDefault("server.port", 8080)
}

// LoadAll creates a single viper instance and calls LoadConfig on each
// Configurable in order, sharing the one parsed config across all of them.
// If a Configurable also implements PostConfigLoader, PostLoadConfig is called
// immediately after its LoadConfig succeeds.
func LoadAll(envPrefix string, cfgs ...Configurable) error {
	v, err := NewViper(envPrefix)
	if err != nil {
		return err
	}
	SetAppDefaults(v)
	l := &ConfigLoader{v: v}
	for _, cfg := range cfgs {
		if err := cfg.LoadConfig(l); err != nil {
			return err
		}
		if pl, ok := cfg.(PostConfigLoader); ok {
			if err := pl.PostLoadConfig(); err != nil {
				return err
			}
		}
	}
	return nil
}

// Load unmarshals the full config file into dest, applying app-level defaults.
// For loading multiple typed sections with a single viper parse, use LoadAll.
func Load(dest any, envPrefix string) error {
	v, err := NewViper(envPrefix)
	if err != nil {
		return err
	}
	SetAppDefaults(v)
	if err := v.Unmarshal(dest); err != nil {
		return fmt.Errorf("error unmarshaling config: %w", err)
	}
	return nil
}

// LoadSection loads a single named config section using the shared viper setup.
// For loading multiple sections with a single viper parse, use LoadAll.
func LoadSection[T any](section, envPrefix string) (*T, error) {
	v, err := NewViper(envPrefix)
	if err != nil {
		return nil, err
	}
	var cfg T
	if err := v.UnmarshalKey(section, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling [%s]: %w", section, err)
	}
	return &cfg, nil
}