package host

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

// WithPostgreSQL registers a database service that forces the PostgreSQL driver.
// Config is loaded from [database] (default) or [database.<name>] at Init time.
// Pass a name to register a named database; no args registers the default.
func (a *App) WithPostgreSQL(name ...string) *App {
	if a.err != nil {
		return a
	}
	n := firstOrEmpty(name)
	a.container.Store(dbKey(n), &databaseService{name: n, driver: "postgres", logger: a.Logger})
	return a
}

func newPostgreSQL(d *databaseService) (*gorm.DB, error) {
	cfg := &d.config
	d.logger.Debug("connecting to postgresql",
		"host", cfg.Host,
		"port", cfg.Port,
		"db", cfg.Name,
		"sslmode", cfg.SSLMode,
	)

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Name, cfg.SSLMode)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:               newGormLogger(d.logger),
		FullSaveAssociations: false,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgresql: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get db connection: %w", err)
	}
	if err = sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetConnMaxLifetime(time.Hour)

	d.logger.Info("connected to postgresql",
		"host", cfg.Host,
		"port", cfg.Port,
		"db", cfg.Name,
	)
	return db, nil
}

// newGormLogger creates a GORM logger that routes SQL logs through the slog logger.
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

// slogWriter adapts slog.Logger to the io.Writer / log.Logger interface expected by GORM.
type slogWriter struct {
	logger *slog.Logger
}

func (w *slogWriter) Printf(format string, args ...any) {
	msg := strings.TrimRight(fmt.Sprintf(format, args...), "\n")
	w.logger.Debug(msg)
}
