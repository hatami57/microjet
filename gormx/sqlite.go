package gormx

import (
	"fmt"
	"log/slog"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type sqliteDriver struct{}

// SQLite returns the built-in SQLite Driver (pure-Go, no cgo). The database file
// path is taken from cfg.Name; use ":memory:" for an in-memory database:
//
//	app.WithDatabase(gormx.SQLite())
//	app.WithNamedDatabase("bot", gormx.SQLite())
func SQLite() Driver { return sqliteDriver{} }

func (sqliteDriver) Open(cfg Config, log *slog.Logger) (*gorm.DB, error) {
	log.Debug("connecting to sqlite", "path", cfg.Name)

	db, err := gorm.Open(sqlite.Open(cfg.Name), &gorm.Config{
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

	log.Info("connected to sqlite", "path", cfg.Name)
	return db, nil
}
