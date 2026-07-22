// Package postgres provides the PostgreSQL driver for gormx.Module.
package postgres

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/hatami57/microjet/core/errorx"
	"github.com/hatami57/microjet/gormx"
	gormpg "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type postgresDriver struct{}

// Driver returns the built-in PostgreSQL driver (pgx). Connection settings are
// read from the config section the host resolves (host, port, user, password,
// name, sslMode):
//
//	app.WithModule(gormx.Module(postgres.Driver()))
//	app.WithModule(gormx.Module(postgres.Driver(), "analytics"))
func Driver() gormx.Driver { return postgresDriver{} }

func (postgresDriver) Open(cfg gormx.Config, log *slog.Logger) (*gorm.DB, error) {
	log.Debug("connecting to postgresql",
		"host", cfg.Host,
		"port", cfg.Port,
		"db", cfg.Name,
		"sslmode", cfg.SSLMode,
	)

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Name, cfg.SSLMode)

	db, err := gorm.Open(gormpg.Open(dsn), &gorm.Config{
		Logger:               gormx.NewGormLogger(log, cfg.LogLevel),
		FullSaveAssociations: false,
		TranslateError:       true,
	})
	if err != nil {
		return nil, errorx.NewInternalError("gormx", "failed to connect to postgresql").WithInner(err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, errorx.NewInternalError("gormx", "failed to get db connection").WithInner(err)
	}
	if err = sqlDB.Ping(); err != nil {
		return nil, errorx.NewInternalError("gormx", "failed to ping database").WithInner(err)
	}
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetConnMaxLifetime(time.Hour)

	log.Info("connected to postgresql", "host", cfg.Host, "port", cfg.Port, "db", cfg.Name)
	return db, nil
}
