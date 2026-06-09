package gormx

import (
	"log/slog"

	"gorm.io/gorm"
)

// Driver opens a *gorm.DB from a resolved Config. It is the extension point for
// plugging a database engine into the host. Implement Open and pass the value to
// host.WithDatabase / host.WithNamedDatabase. The host owns config resolution and
// lifecycle (Close, health checks); a Driver is responsible only for dialing.
//
// Use Postgres() or SQLite() for the built-in engines.
type Driver interface {
	Open(cfg Config, log *slog.Logger) (*gorm.DB, error)
}
