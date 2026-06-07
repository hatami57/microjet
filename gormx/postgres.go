package gormx

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

type postgresDriver struct{}

// Postgres returns the built-in PostgreSQL Driver (pgx). Connection settings are
// read from the config section the host resolves (host, port, user, password,
// name, sslMode):
//
//	app.WithDatabase(gormx.Postgres())
//	app.WithNamedDatabase("analytics", gormx.Postgres())
func Postgres() Driver { return postgresDriver{} }

func (postgresDriver) Open(cfg Config, log *slog.Logger) (*gorm.DB, error) {
	log.Debug("connecting to postgresql",
		"host", cfg.Host,
		"port", cfg.Port,
		"db", cfg.Name,
		"sslmode", cfg.SSLMode,
	)

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Name, cfg.SSLMode)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:               newGormLogger(log),
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

	log.Info("connected to postgresql", "host", cfg.Host, "port", cfg.Port, "db", cfg.Name)
	return db, nil
}

// newGormLogger routes GORM SQL logs through slog.
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
