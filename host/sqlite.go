package host

import (
	"fmt"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// WithSQLite connects to a SQLite database and registers the connection as the
// default database. The database file path is taken from the [database] config
// section's "name" field; use ":memory:" for an in-memory database. Errors are
// deferred to Run/MustRun/Err.
//
// It uses the pure-Go glebarez/sqlite driver, so no cgo or C toolchain is
// required.
func (a *App) WithSQLite() *App {
	if a.err != nil {
		return a
	}
	if a.Config.Database == nil {
		return a.fail(fmt.Errorf("sqlite: no [database] config section"))
	}
	db, err := newSQLite(a)
	if err != nil {
		return a.fail(fmt.Errorf("sqlite: %w", err))
	}
	return a.WithDatabase(db)
}

func newSQLite(a *App) (*gorm.DB, error) {
	path := a.Config.Database.Name
	a.Logger.Debug("connecting to sqlite", "path", path)

	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger:               newGormLogger(a.Logger),
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

	a.Logger.Info("connected to sqlite", "path", path)
	return db, nil
}
