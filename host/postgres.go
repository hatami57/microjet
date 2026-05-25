package host

import (
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

// WithPostgreSQL connects to PostgreSQL using the [database] config section and
// assigns the gorm handle to App.DB. Connection errors are deferred to
// Run/MustRun/Err.

var logLevelMap = map[string]gormLogger.LogLevel{
	"silent": gormLogger.Silent,
	"error":  gormLogger.Error,
	"warn":   gormLogger.Warn,
	"info":   gormLogger.Info,
}

func (a *App) WithPostgreSQL() *App {
	if a.err != nil {
		return a
	}
	db, err := newPostgreSQL(a)
	if err != nil {
		return a.fail(fmt.Errorf("postgres: %w", err))
	}
	a.DB = db
	return a
}

func newPostgreSQL(a *App) (*gorm.DB, error) {
	logLevel, ok := logLevelMap[a.Config.Log.Level]
	if !ok {
		logLevel = gormLogger.Info
	}

	gormLog := gormLogger.New(
		log.New(os.Stdout, "\n", log.LstdFlags),
		gormLogger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  logLevel,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
		},
	)

	dbCfg := a.Config.Database
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		dbCfg.Host, dbCfg.Port, dbCfg.User, dbCfg.Password, dbCfg.Name, dbCfg.SSLMode)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:               gormLog,
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

	return db, nil
}
