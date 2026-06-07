package host

import (
	"fmt"
	"log/slog"

	"github.com/glebarez/sqlite"
	"github.com/hatami57/microjet/gormx"
	"gorm.io/gorm"
)

// sqliteDriver is the built-in SQLite Driver, using the pure-Go glebarez/sqlite
// driver (no cgo required).
type sqliteDriver struct{}

// SQLite returns the built-in SQLite Driver. The database file path is taken
// from the config's "name" field; use ":memory:" for an in-memory database:
//
//	app.WithDatabase(host.SQLite())
//	app.WithNamedDatabase("bot", host.SQLite())
func SQLite() Driver { return sqliteDriver{} }

// WithSQLite registers a default-or-named database forced to SQLite.
//
// Deprecated: use WithDatabase(host.SQLite()) or
// WithNamedDatabase(name, host.SQLite()) instead.
func (a *App) WithSQLite(name ...string) *App {
	return a.WithNamedDatabase(firstOrEmpty(name), SQLite())
}

func (sqliteDriver) Open(cfg gormx.Config, log *slog.Logger) (*gorm.DB, error) {
	path := cfg.Name
	log.Debug("connecting to sqlite", "path", path)

	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger:               newGormLogger(log),
		FullSaveAssociations: false,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to sqlite database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get db connection: %w", err)
	}
	if err = sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	// SQLite handles a single writer at a time; a single connection avoids
	// "database is locked" errors under concurrent access.
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetMaxOpenConns(1)

	log.Info("connected to sqlite", "path", path)
	return db, nil
}
