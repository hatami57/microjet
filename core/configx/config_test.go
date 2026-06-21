package configx

import (
	"os"
	"path/filepath"
	"testing"
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

func TestReadAllClaimsEverything(t *testing.T) {
	r := readerWithConfig(t, "[anything]\nx = 1\n")
	var dst map[string]any
	_ = r.ReadAll(&dst)
	if unused := r.UnusedSections(); unused != nil {
		t.Fatalf("UnusedSections() after ReadAll = %v, want nil", unused)
	}
}
