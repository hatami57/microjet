package host

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/hatami57/microjet/gormx"
	"gorm.io/gorm"
)

// Driver opens a *gorm.DB from a resolved configuration. It is the extension
// point for plugging a database engine into the host: implement Open and pass
// the value to WithDatabase / WithNamedDatabase. The host owns config resolution
// (it loads the [database] / [database.<name>] section into cfg) and lifecycle
// (Close, health checks); a Driver is responsible only for dialing.
//
// The built-in Postgres and SQLite return Driver values, and AutoDriver selects
// one of them from cfg.Driver at Open time. To use an already-built connection
// (e.g. in tests) call InjectDatabase instead of implementing a Driver.
type Driver interface {
	Open(cfg gormx.Config, log *slog.Logger) (*gorm.DB, error)
}

// DBOption tunes a single database registration. Options are applied to the
// service before it is stored, so they can override values derived from the
// registration name.
type DBOption func(*databaseService)

// Section overrides the config section a database loads. By default the section
// is derived from the registration name ([database] for the default database,
// [database.<name>] for a named one); use Section when the logical name and the
// config section differ, e.g. WithNamedDatabase("bot", SQLite(), Section("legacy")).
func Section(name string) DBOption {
	return func(d *databaseService) { d.section = name }
}

// autoDriver selects the concrete built-in driver from cfg.Driver at Open time.
// It backs the config-driven registrations (WithDatabaseFromConfig /
// WithDatabasesFromConfig), where the engine is chosen by configuration rather
// than at the call site.
type autoDriver struct{}

// AutoDriver returns a Driver that dispatches on the "driver" config field
// (postgres, sqlite). Use it when the database engine is selected by config.
func AutoDriver() Driver { return autoDriver{} }

func (autoDriver) Open(cfg gormx.Config, log *slog.Logger) (*gorm.DB, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Driver)) {
	case "postgres", "postgresql":
		return postgresDriver{}.Open(cfg, log)
	case "sqlite", "sqlite3":
		return sqliteDriver{}.Open(cfg, log)
	case "":
		return nil, fmt.Errorf("no driver configured (set driver in [database] config)")
	default:
		return nil, fmt.Errorf("unsupported driver %q", cfg.Driver)
	}
}
