package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

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

// SetDefault registers a default value for a config key. Configurables call
// this before UnmarshalKey so their defaults apply when no config file is present.
func (l *ConfigLoader) SetDefault(key string, value any) {
	l.v.SetDefault(key, value)
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

// Initer is implemented by services that need to perform initialization after
// their config is loaded but do not require host-level DI. The host calls Init
// on each registered service that implements this
// interface (host.ServiceIniter, which carries *App, takes precedence).
type Initer interface {
	Init() error
}

// Closer is implemented by services that need to release resources on shutdown.
// The host calls Close on each registered service that implements
// this interface (host.ServiceCloser takes precedence when present).
type Closer interface {
	Close() error
}

// ConfigurableFunc is a function adapter for Configurable, analogous to http.HandlerFunc.
type ConfigurableFunc func(*ConfigLoader) error

func (f ConfigurableFunc) LoadConfig(l *ConfigLoader) error { return f(l) }

// NewViper builds the viper instance microjet uses to load configuration:
// it searches the standard config paths, reads config.toml plus an optional
// config.local.toml overlay, and binds APP_* environment overrides. It is
// exported so provider-specific modules (e.g. aws) can build their own config
// loading without core having to depend on them.
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

// LoadAll creates a single viper instance and calls LoadConfig on each
// Configurable in order, sharing the one parsed config across all of them.
// If a Configurable also implements PostConfigLoader, PostLoadConfig is called
// immediately after its LoadConfig succeeds.
func LoadAll(envPrefix string, cfgs ...Configurable) error {
	v, err := NewViper(envPrefix)
	if err != nil {
		return err
	}
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
