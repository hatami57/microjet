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

// WithPostgreSQL connects to PostgreSQL using the [database] config section and
// registers the connection as the default database. Errors are deferred to
// Run/MustRun/Err.
func (a *App) WithPostgreSQL() *App {
	if a.err != nil {
		return a
	}
	db, err := newPostgreSQL(a)
	if err != nil {
		return a.fail(fmt.Errorf("postgres: %w", err))
	}
	return a.WithDatabase(db)
}

func newPostgreSQL(a *App) (*gorm.DB, error) {
	dbCfg := a.Config.Database
	a.Logger.Debug("connecting to postgresql",
		"host", dbCfg.Host,
		"port", dbCfg.Port,
		"db", dbCfg.Name,
		"sslmode", dbCfg.SSLMode,
	)

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		dbCfg.Host, dbCfg.Port, dbCfg.User, dbCfg.Password, dbCfg.Name, dbCfg.SSLMode)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:               newGormLogger(a.Logger),
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

	a.Logger.Info("connected to postgresql",
		"host", dbCfg.Host,
		"port", dbCfg.Port,
		"db", dbCfg.Name,
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
