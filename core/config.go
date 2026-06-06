package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	App      *AppConfig      `mapstructure:"app"`
	Server   *ServerConfig   `mapstructure:"server"`
	Database *DatabaseConfig `mapstructure:"database"`
	// Databases holds additional named connections from the [databases.<name>]
	// config tables, registered via host.WithDatabasesFromConfig. The single
	// [database] section above remains the default connection.
	Databases map[string]*DatabaseConfig `mapstructure:"databases"`
	Messaging *MessagingConfig           `mapstructure:"messaging"`
	Log       *LogConfig                 `mapstructure:"log"`
	Extra     any                        `mapstructure:"extra"`
}

type MessagingConfig struct {
	URL     string `mapstructure:"url"`
	Source  string `mapstructure:"source"`
	Version int    `mapstructure:"version"`
}

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

type DatabaseConfig struct {
	Driver   string `mapstructure:"driver"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Name     string `mapstructure:"name"`
	SSLMode  string `mapstructure:"sslMode"`
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

// LoadConfig loads application configuration from a TOML file.
// Searches cwd, cwd/config, executable dir, and executable_dir/config.
// A config.local.toml in the same directories is merged on top if present.
// Environment variables prefixed with APP_ override any file value (dots → underscores).
func LoadConfig(envPrefix string) (*Config, error) {
	return LoadConfigWithExtra[map[string]any](envPrefix)
}

// LoadConfigWithExtra is like LoadConfig but unmarshals the [extra] TOML section into T.
// Use host.NewWithExtraConfig[T] at service startup instead of calling this directly.
func LoadConfigWithExtra[T any](envPrefix string) (*Config, error) {
	type raw struct {
		App       *AppConfig                 `mapstructure:"app"`
		Server    *ServerConfig              `mapstructure:"server"`
		Database  *DatabaseConfig            `mapstructure:"database"`
		Databases map[string]*DatabaseConfig `mapstructure:"databases"`
		Messaging *MessagingConfig           `mapstructure:"messaging"`
		Log       *LogConfig                 `mapstructure:"log"`
		Extra     T                          `mapstructure:"extra"`
	}
	r := &raw{}
	if err := Load(r, envPrefix); err != nil {
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

// ConfigViper builds the viper instance microjet uses to load configuration:
// it searches the standard config paths, reads config.toml plus an optional
// config.local.toml overlay, and binds APP_* environment overrides. It is
// exported so provider-specific modules (e.g. aws) can load their own typed
// config sections via Get/UnmarshalKey without core having to depend on them.
func ConfigViper(envPrefix string) (*viper.Viper, error) {
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
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
	}

	v.SetConfigName("config.local")
	_ = v.MergeInConfig() // optional overlay; absence is fine

	return v, nil
}

func Load(dest any, envPrefix string) error {
	v, err := ConfigViper(envPrefix)
	if err != nil {
		return err
	}

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

// Extra config helpers

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
