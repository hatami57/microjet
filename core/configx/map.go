package configx

import "github.com/spf13/viper"

// mapReader is a Reader backed by an in-memory value tree instead of TOML files
// and environment variables. The values it is constructed with are
// authoritative, with SetDefault filling any gaps and Set overriding on top. It
// performs no filesystem discovery and consults no environment variables, so it
// suits embedding an App in a host process that already owns configuration, or
// supplying fixed configuration in tests. Construct it with NewMapReader and
// inject it via host.WithConfigReader.
type mapReader struct {
	v *viper.Viper
}

// NewMapReader returns an in-memory Reader seeded from values — a nested map
// mirroring the config-file layout, for example:
//
//	configx.NewMapReader(map[string]any{
//	    "app": map[string]any{"name": "billing", "shutdownDelay": "5s"},
//	    "log": map[string]any{"level": "debug"},
//	})
//
// Seeded values win over SetDefault, and string values decode into typed fields
// exactly as the file reader does (so "5s" populates a time.Duration).
// Environment variables and the filesystem are ignored: the map is
// authoritative. A nil or empty values map yields a reader that returns only
// the defaults each Configurable registers.
func NewMapReader(values map[string]any) Reader {
	v := viper.New()
	if len(values) > 0 {
		// MergeConfigMap deep-merges into viper's config layer, which outranks
		// defaults and is the layer Unmarshal reads — the behavior Read/ReadAll
		// depend on. A plain nested map cannot fail the merge.
		_ = v.MergeConfigMap(values)
	}
	return &mapReader{v: v}
}

// SetDefault registers a fallback value used when the seeded map has no entry
// for key.
func (r *mapReader) SetDefault(key string, value any) { r.v.SetDefault(key, value) }

// Read unmarshals the subtree at the dotted key into dest, decoding strings to
// typed fields (durations, slices) as the file reader does.
func (r *mapReader) Read(key string, dest any) error { return r.v.UnmarshalKey(key, dest) }

// ReadMap returns the subtree at key as a map; sub-tables are nested maps.
func (r *mapReader) ReadMap(key string) map[string]any { return r.v.GetStringMap(key) }

// ReadAll unmarshals the whole value tree into dest.
func (r *mapReader) ReadAll(dest any) error { return r.v.Unmarshal(dest) }

// Set records an override that wins over the seeded values and defaults,
// satisfying Setter so host.WithConfigValue can layer values on top.
func (r *mapReader) Set(key string, value any) { r.v.Set(key, value) }
