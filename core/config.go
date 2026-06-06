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

type LoaderOption func(any) error

var postLoadHooks []LoaderOption

func RegisterPostLoadHook(hook LoaderOption) {
	postLoadHooks = append(postLoadHooks, hook)
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

// NewViper builds the viper instance microjet uses to load configuration:
// it searches the standard config paths, reads config.toml plus an optional
// config.local.toml overlay, and binds APP_* environment overrides. It is
// exported so provider-specific modules (e.g. aws) can load their own typed
// config sections via Get/UnmarshalKey without core having to depend on them.
// It does not set any app-level defaults; call Load to get those.
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

// setAppDefaults sets the core app/server defaults on v. Kept separate from
// NewViper so provider modules (aws, etc.) that call NewViper directly
// don't pick up defaults that are irrelevant to their config sections.
func setAppDefaults(v *viper.Viper) {
	v.SetDefault("app.namespace", "App")
	v.SetDefault("app.environment", "development")
	v.SetDefault("app.name", "App")
	v.SetDefault("app.version", "0.1.0")
	// Default to non-debug: debug mode enables gin's verbose mode, Swagger, and
	// inner-error exposure in HTTP responses — none of which are safe defaults
	// for a library that may be embedded in production. Opt in via app.debug=true.
	v.SetDefault("app.debug", false)
	v.SetDefault("server.host", "localhost")
	v.SetDefault("server.port", 8080)
}

// Load unmarshals the full config file into dest, applying app-level defaults.
// Most callers should use host.LoadConfig instead; this is the low-level entry
// point for custom unmarshaling or test setups.
func Load(dest any, envPrefix string) error {
	v, err := NewViper(envPrefix)
	if err != nil {
		return err
	}
	setAppDefaults(v)

	if err := v.Unmarshal(dest); err != nil {
		return fmt.Errorf("error unmarshaling config: %w", err)
	}

	for _, hook := range postLoadHooks {
		if err := hook(dest); err != nil {
			return fmt.Errorf("error running post-load hook: %w", err)
		}
	}

	return nil
}

// LoadSection loads a single named config section (e.g. "aws", "database") using
// the shared viper setup. It is the generic counterpart to Load for provider
// modules that own only one section of the config file.
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
