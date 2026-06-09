package sqlite

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hatami57/microjet/gormx"
	glebarez "github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

type sqliteDriver struct{}

// Driver returns the built-in SQLite driver (pure-Go, no cgo). The database
// file path is taken from cfg.Name; use ":memory:" for an in-memory database:
//
//	app.WithDatabase(sqlite.Driver())
//	app.WithNamedDatabase("bot", sqlite.Driver())
func Driver() gormx.Driver { return sqliteDriver{} }

func (sqliteDriver) Open(cfg gormx.Config, log *slog.Logger) (*gorm.DB, error) {
	log.Debug("connecting to sqlite", "path", cfg.Name)

	db, err := gorm.Open(glebarez.Open(cfg.Name), &gorm.Config{
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

func newGormLogger(sl *slog.Logger) gormLogger.Interface {
	level := gormLogger.Info
	ctx := context.Background()
	switch {
	case !sl.Enabled(ctx, slog.LevelInfo) && sl.Enabled(ctx, slog.LevelWarn):
		level = gormLogger.Warn
	case !sl.Enabled(ctx, slog.LevelWarn):
		level = gormLogger.Error
	}
	return gormLogger.New(
		&slogWriter{logger: sl},
		gormLogger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  level,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)
}

type slogWriter struct{ logger *slog.Logger }

func (w *slogWriter) Printf(format string, args ...any) {
	w.logger.Debug(strings.TrimRight(fmt.Sprintf(format, args...), "\n"))
}
