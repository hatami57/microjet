package configx

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/hatami57/microjet/core/errorx"
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

// Set records an override that wins over config files, environment variables,
// and defaults — the top of viper's precedence order — so it is authoritative.
// It satisfies Setter, which host.WithConfigValue uses to inject settings in
// code without a config file or environment variable.
func (r *viperConfigReader) Set(key string, value any) {
	r.v.Set(key, value)
}

// Read unmarshals the named config key into dest, recording the key's top-level
// section as claimed. Environment variables override file and default values
// (see applyEnvOverrides).
func (r *viperConfigReader) Read(key string, dest any) error {
	r.claimed[topSection(key)] = true
	r.applyEnvOverrides(key, reflect.TypeOf(dest))
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
	r.applyEnvOverrides("", reflect.TypeOf(dest))
	return r.v.Unmarshal(dest)
}

// applyEnvOverrides makes Unmarshal/UnmarshalKey honor environment variables.
// Viper consults AutomaticEnv when Get is called on a leaf key, but Unmarshal
// assembles a section from the config-file and default layers only — so env vars
// silently have no effect on a struct unmarshal. To fix that we bind an env var
// for every leaf field of dest, then promote each known key's resolved value
// (env > file > default) into viper's override layer, which Unmarshal does read.
//
// section is the dotted config key dest maps to ("" when dest is the whole
// config, as in ReadAll).
func (r *viperConfigReader) applyEnvOverrides(section string, t reflect.Type) {
	bindEnvLeaves(r.v, section, t)
	prefix := section + "."
	for _, k := range r.v.AllKeys() {
		if section != "" && k != section && !strings.HasPrefix(k, prefix) {
			continue
		}
		if r.v.IsSet(k) {
			r.v.Set(k, r.v.Get(k))
		}
	}
}

// bindEnvLeaves walks struct type t and binds an environment variable for every
// leaf field, building dotted keys from mapstructure tags (falling back to the
// lowercased field name) under prefix. Binding registers env-only keys with
// viper so their values are picked up even when absent from the config file.
func bindEnvLeaves(v *viper.Viper, prefix string, t reflect.Type) {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return
	}
	if t.Kind() != reflect.Struct || t == reflect.TypeFor[time.Time]() {
		if prefix != "" {
			_ = v.BindEnv(prefix)
		}
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag, _, _ := strings.Cut(f.Tag.Get("mapstructure"), ",")
		if tag == "-" {
			continue
		}
		if tag == "" {
			tag = strings.ToLower(f.Name)
		}
		key := tag
		if prefix != "" {
			key = prefix + "." + tag
		}
		bindEnvLeaves(v, key, f.Type)
	}
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
			return nil, errorx.NewInternalError("config", "reading config file").WithInner(err)
		}
	}

	v.SetConfigName("config.local")
	_ = v.MergeInConfig() // optional overlay; absence is fine

	return v, nil
}
