package configx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/viper"
)

type viperConfigReader struct {
	v *viper.Viper

	// fileSections are the top-level tables that came from the config file(s),
	// captured before any defaults were registered. claimed records which were
	// actually read; the difference is reported by UnusedSections to catch typos
	// and renamed sections that silently have no effect.
	fileSections map[string]bool
	claimed      map[string]bool
	claimedAll   bool
}

// NewViperConfigReader creates a Reader. Use this to hold a single reader across
// multiple Configure calls (e.g. in App.configReader) so the config file is only read once.
func NewViperConfigReader(envPrefix string) (Reader, error) {
	v, err := newViper(envPrefix)
	if err != nil {
		return nil, err
	}
	// Capture the file's top-level sections now, before any SetDefault call adds
	// synthetic keys — so unused-section detection reflects the file alone.
	fileSections := make(map[string]bool)
	for _, key := range v.AllKeys() {
		fileSections[topSection(key)] = true
	}
	return &viperConfigReader{v: v, fileSections: fileSections, claimed: make(map[string]bool)}, nil
}

// SetDefault registers a default value for a config key. Configurables call
// this before UnmarshalKey so their defaults apply when no config file is present.
func (r *viperConfigReader) SetDefault(key string, value any) {
	r.v.SetDefault(key, value)
}

// Read unmarshals the named config key into dest, recording the key's top-level
// section as claimed.
func (r *viperConfigReader) Read(key string, dest any) error {
	r.claimed[topSection(key)] = true
	return r.v.UnmarshalKey(key, dest)
}

// ReadMap returns all keys and their values under a config key.
// Sub-tables appear as map[string]any values, scalars as their native types.
func (r *viperConfigReader) ReadMap(key string) map[string]any {
	r.claimed[topSection(key)] = true
	return r.v.GetStringMap(key)
}

func (r *viperConfigReader) ReadAll(dest any) error {
	r.claimedAll = true
	return r.v.Unmarshal(dest)
}

// UnusedSections returns the config-file top-level sections that no Read/ReadMap
// call consumed, sorted. A non-empty result usually means a typo or a renamed
// section (e.g. a stale [server] after it became [http]) that silently has no
// effect. ReadAll claims everything, so this is empty once ReadAll is used.
func (r *viperConfigReader) UnusedSections() []string {
	if r.claimedAll {
		return nil
	}
	var unused []string
	for section := range r.fileSections {
		if !r.claimed[section] {
			unused = append(unused, section)
		}
	}
	sort.Strings(unused)
	return unused
}

// topSection returns the first dotted segment of a config key — its top-level
// table. Viper lowercases keys, so comparisons are case-insensitive.
func topSection(key string) string {
	top, _, _ := strings.Cut(key, ".")
	return top
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
