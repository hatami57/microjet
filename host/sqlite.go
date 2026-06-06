package host

import (
	"fmt"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// WithSQLite registers a database service that forces the SQLite driver.
// Config is loaded from [database] (default) or [database.<name>] at Init time.
// The database file path is taken from the config's "name" field; use ":memory:"
// for an in-memory database. Pass a name to register a named database; no args
// registers the default. Uses the pure-Go glebarez/sqlite driver (no cgo needed).
func (a *App) WithSQLite(name ...string) *App {
	if a.err != nil {
		return a
	}
	n := firstOrEmpty(name)
	a.container.Store(dbKey(n), &databaseService{name: n, driver: "sqlite", logger: a.Logger})
	return a
}

func newSQLite(d *databaseService) (*gorm.DB, error) {
	path := d.config.Name
	d.logger.Debug("connecting to sqlite", "path", path)

	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger:               newGormLogger(d.logger),
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

	d.logger.Info("connected to sqlite", "path", path)
	return db, nil
}
