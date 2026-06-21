package httpx

import (
	"testing"

	"github.com/hatami57/microjet/host"
)

// countServers reports how many *Server instances are registered in the app's
// container, across all names.
func countServers(app *host.App) int {
	n := 0
	app.RangeServices(func(svc any) bool {
		if _, ok := svc.(*Server); ok {
			n++
		}
		return true
	})
	return n
}

func TestModuleDoubleInstallDeduplicates(t *testing.T) {
	app, err := host.New()
	if err != nil {
		t.Fatalf("host.New: %v", err)
	}
	// Installing the default server twice must register exactly one — the second
	// install deduplicates rather than silently clobbering the first.
	app.WithModule(Module()).WithModule(Module())
	if err := app.Err(); err != nil {
		t.Fatalf("WithModule: %v", err)
	}
	if got := countServers(app); got != 1 {
		t.Fatalf("server count after double default install = %d, want 1", got)
	}
}

func TestNamedModulesCoexist(t *testing.T) {
	app, err := host.New()
	if err != nil {
		t.Fatalf("host.New: %v", err)
	}
	// Distinct names are distinct slots, so both install.
	app.WithModule(Module()).WithModule(Module("admin"))
	if err := app.Err(); err != nil {
		t.Fatalf("WithModule: %v", err)
	}
	if got := countServers(app); got != 2 {
		t.Fatalf("server count for default + admin = %d, want 2", got)
	}
	if Of(app) == Of(app, "admin") {
		t.Fatal("default and admin servers should be distinct instances")
	}
}
