package configx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hatami57/microjet/core/errorx"
)

func TestNewViperDoesNotErrorWithNoFile(t *testing.T) {
	if _, err := newViper(""); err != nil {
		t.Fatalf("NewViper returned error with no config file: %v", err)
	}
}

// readerWithConfig writes a config.toml in a temp dir, chdirs there, and returns
// a reader over it.
func readerWithConfig(t *testing.T, toml string) *viperConfigReader {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(toml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Chdir(dir)
	r, err := NewViperConfigReader("")
	if err != nil {
		t.Fatalf("NewViperConfigReader: %v", err)
	}
	return r.(*viperConfigReader)
}

func TestUnusedSectionsReportsOrphanSection(t *testing.T) {
	r := readerWithConfig(t, "[http]\nport = 9000\n\n[server]\nport = 8080\n")

	var cfg struct {
		Port int `mapstructure:"port"`
	}
	if err := r.Read("http", &cfg); err != nil {
		t.Fatalf("Read: %v", err)
	}

	unused := r.UnusedSections()
	if len(unused) != 1 || unused[0] != "server" {
		t.Fatalf("UnusedSections() = %v, want [server]", unused)
	}
}

func TestUnusedSectionsEmptyWhenAllRead(t *testing.T) {
	r := readerWithConfig(t, "[http]\nport = 9000\n[database]\ndriver = \"sqlite\"\n")
	var dst map[string]any
	_ = r.Read("http", &dst)
	_ = r.Read("database", &dst)
	if unused := r.UnusedSections(); len(unused) != 0 {
		t.Fatalf("UnusedSections() = %v, want none", unused)
	}
}

func TestEnvOverridesFileAndDefault(t *testing.T) {
	r := readerWithConfig(t, "[payments]\ncurrency = \"USD\"\nmaxRetries = 3\n")

	// Env overrides a file value, supplies an env-only value (no file/default),
	// and overrides a nested value; an unset field keeps its file value.
	t.Setenv("APP_PAYMENTS_CURRENCY", "EUR")     // overrides file
	t.Setenv("APP_PAYMENTS_SANDBOX", "true")     // env-only (absent from file)
	t.Setenv("APP_PAYMENTS_NESTED_MODE", "live") // nested env

	var cfg struct {
		Currency   string `mapstructure:"currency"`
		MaxRetries int    `mapstructure:"maxRetries"`
		Sandbox    bool   `mapstructure:"sandbox"`
		Nested     struct {
			Mode string `mapstructure:"mode"`
		} `mapstructure:"nested"`
	}
	if err := r.Read("payments", &cfg); err != nil {
		t.Fatalf("Read: %v", err)
	}

	if cfg.Currency != "EUR" {
		t.Errorf("Currency = %q, want EUR (env should override file)", cfg.Currency)
	}
	if !cfg.Sandbox {
		t.Error("Sandbox = false, want true (env-only value should apply)")
	}
	if cfg.Nested.Mode != "live" {
		t.Errorf("Nested.Mode = %q, want live (nested env should apply)", cfg.Nested.Mode)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3 (file value should survive)", cfg.MaxRetries)
	}
}

// validatingConfig reads a [svc] section and validates that Port is set.
type validatingConfig struct {
	Port int `mapstructure:"port"`
}

func (c *validatingConfig) ReadConfig(r Reader) error { return r.Read("svc", c) }

func (c *validatingConfig) Validate() error {
	if c.Port == 0 {
		return errors.New("port must be set")
	}
	return nil
}

func TestReadAndValidateRunsValidate(t *testing.T) {
	r := readerWithConfig(t, "[svc]\nport = 0\n")
	err := ReadAndValidate(r, &validatingConfig{})
	if err == nil {
		t.Fatal("ReadAndValidate() = nil, want validation error")
	}
	if !errors.Is(err, errorx.ErrInternal) {
		t.Fatalf("ReadAndValidate() error = %v, want an errorx internal error", err)
	}
	if got := err.Error(); !strings.Contains(got, "config validation failed") || !strings.Contains(got, "port must be set") {
		t.Fatalf("ReadAndValidate() error = %q, want it to mention the wrapper and the inner cause", got)
	}
}

func TestReadAndValidatePassesWhenValid(t *testing.T) {
	r := readerWithConfig(t, "[svc]\nport = 8080\n")
	cfg := &validatingConfig{}
	if err := ReadAndValidate(r, cfg); err != nil {
		t.Fatalf("ReadAndValidate() = %v, want nil", err)
	}
	if cfg.Port != 8080 {
		t.Fatalf("Port = %d, want 8080", cfg.Port)
	}
}

// plainConfig has no Validate method; ReadAndValidate must still populate it.
type plainConfig struct {
	Port int `mapstructure:"port"`
}

func (c *plainConfig) ReadConfig(r Reader) error { return r.Read("svc", c) }

func TestReadAndValidateSkipsWhenNoValidator(t *testing.T) {
	r := readerWithConfig(t, "[svc]\nport = 0\n")
	if err := ReadAndValidate(r, &plainConfig{}); err != nil {
		t.Fatalf("ReadAndValidate() = %v, want nil (no Validator implemented)", err)
	}
}

func TestReadAllClaimsEverything(t *testing.T) {
	r := readerWithConfig(t, "[anything]\nx = 1\n")
	var dst map[string]any
	_ = r.ReadAll(&dst)
	if unused := r.UnusedSections(); unused != nil {
		t.Fatalf("UnusedSections() after ReadAll = %v, want nil", unused)
	}
}
