package host

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hatami57/microjet/gormx"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

// postgresDriver is the built-in PostgreSQL Driver (gorm + pgx).
type postgresDriver struct{}

// Postgres returns the built-in PostgreSQL Driver. Connection settings are read
// from the database config section (host, port, user, password, name, sslMode):
//
//	app.WithDatabase(host.Postgres())
//	app.WithNamedDatabase("bot", host.Postgres())
func Postgres() Driver { return postgresDriver{} }

func (postgresDriver) Open(cfg gormx.Config, log *slog.Logger) (*gorm.DB, error) {
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

	log.Info("connected to postgresql",
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
