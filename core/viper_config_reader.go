package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

type viperConfigReader struct {
	v *viper.Viper
}

// NewViperConfigReader creates a ConfigReader. Use this to hold a single reader across
// multiple Configure calls (e.g. in App.configReader) so the config file is only read once.
func NewViperConfigReader(envPrefix string) (ConfigReader, error) {
	v, err := newViper(envPrefix)
	if err != nil {
		return nil, err
	}
	return &viperConfigReader{v: v}, nil
}

// SetDefault registers a default value for a config key. Configurables call
// this before UnmarshalKey so their defaults apply when no config file is present.
func (r *viperConfigReader) SetDefault(key string, value any) {
	r.v.SetDefault(key, value)
}

// Read unmarshals the named config key into dest.
func (r *viperConfigReader) Read(key string, dest any) error {
	return r.v.UnmarshalKey(key, dest)
}

// ReadMap returns all keys and their values under a config key.
// Sub-tables appear as map[string]any values, scalars as their native types.
func (r *viperConfigReader) ReadMap(key string) map[string]any {
	return r.v.GetStringMap(key)
}

func (r *viperConfigReader) ReadAll(dest any) error {
	return r.v.Unmarshal(dest)
}

func newViper(envPrefix string) (*viper.Viper, error) {
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
