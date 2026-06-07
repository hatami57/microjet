package gormx

import (
	"fmt"
	"log/slog"
	"strings"

	"gorm.io/gorm"
)

// Driver opens a *gorm.DB from a resolved Config. It is the extension point for
// plugging a database engine into the host. Implement Open and pass the value to
// host.WithDatabase / host.WithNamedDatabase. The host owns config resolution and
// lifecycle (Close, health checks); a Driver is responsible only for dialing.
//
// Use Postgres(), SQLite(), or AutoDriver() for the built-in engines.
type Driver interface {
	Open(cfg Config, log *slog.Logger) (*gorm.DB, error)
}

// autoDriver selects the concrete built-in driver from cfg.Driver at Open time.
type autoDriver struct{}

// AutoDriver returns a Driver that dispatches on the "driver" config field
// ("postgres" or "sqlite"). Use it when the database engine is chosen by config
// rather than at the call site — typically via host.WithDatabaseFromConfig.
func AutoDriver() Driver { return autoDriver{} }

func (autoDriver) Open(cfg Config, log *slog.Logger) (*gorm.DB, error) {
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
