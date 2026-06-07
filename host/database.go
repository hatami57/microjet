package host

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/gormx"
	"gorm.io/gorm"
)

// databaseService implements core.Configurable, core.Initer, core.Closer, and
// core.HealthChecker. It resolves its config section, hands the result to its
// Driver to open a connection, and manages that connection's lifecycle. When a
// connection is injected up front (InjectDatabase), db is set and driver is nil:
// config loading and Init become no-ops, but Close and health checks still run.
type databaseService struct {
	name    string
	section string // config section override; empty means derive from name
	driver  gormx.Driver
	config  gormx.Config
	logger  *slog.Logger
	db      *gorm.DB
}

func (d *databaseService) LoadConfig(l *core.ConfigLoader) error {
	if d.db != nil {
		return nil // injected connection; nothing to configure
	}
	return l.UnmarshalKey(d.configKey(), &d.config)
}

// configKey returns the TOML section this database loads, derived from the
// section override or the registration name. The default database maps to
// [database]; a named one to [database.<name>].
func (d *databaseService) configKey() string {
	section := d.section
	if section == "" {
		section = d.name
	}
	if section == "" || section == DefaultDatabase {
		return "database"
	}
	return "database." + section
}

func (d *databaseService) Init() error {
	if d.db != nil {
		return nil
	}
	db, err := d.driver.Open(d.config, d.logger)
	if err != nil {
		return err
	}
	d.db = db
	return nil
}

func (d *databaseService) Close() error {
	if d.db == nil {
		return nil
	}
	sqlDB, err := d.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Healthy implements core.HealthChecker, pinging the underlying connection so the
// host's /readyz can report the database. A self-describing error names the
// database for multi-database setups.
func (d *databaseService) Healthy(ctx context.Context) error {
	if d.db == nil {
		return nil
	}
	name := d.name
	if name == "" {
		name = DefaultDatabase
	}
	sqlDB, err := d.db.DB()
	if err != nil {
		return fmt.Errorf("db %q: %w", name, err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("db %q: %w", name, err)
	}
	return nil
}

// dbKey returns the sync.Map key for a named database.
// An empty name maps to "db:default".
func dbKey(name string) string {
	if name == "" || name == DefaultDatabase {
		return "db:" + DefaultDatabase
	}
	return "db:" + name
}

// DB returns the default database connection, or nil if none is registered.
func (a *App) DB() *gorm.DB {
	return a.NamedDB(DefaultDatabase)
}

// NamedDB returns a named database connection, or nil if not found.
func (a *App) NamedDB(name string) *gorm.DB {
	v, ok := a.container.Load(dbKey(name))
	if !ok {
		return nil
	}
	svc, ok := v.(*databaseService)
	if !ok {
		return nil
	}
	return svc.db
}

// WithDatabase registers driver as the default database. The connection is
// opened during the host's init phase from the [database] config section. Pass a
// built-in driver (gormx.Postgres(), gormx.SQLite()) or any custom gormx.Driver.
// To supply an already-open *gorm.DB instead, use InjectDatabase.
func (a *App) WithDatabase(driver gormx.Driver) *App {
	return a.WithNamedDatabase(DefaultDatabase, driver)
}

// WithNamedDatabase registers driver under name. Config is loaded from
// [database.<name>] unless overridden with Section. Use this to run several
// databases side by side, each retrievable via NamedDB(name).
func (a *App) WithNamedDatabase(name string, driver gormx.Driver) *App {
	if a.err != nil {
		return a
	}
	if driver == nil {
		return a.fail(fmt.Errorf("database %q: nil driver", name))
	}
	svc := &databaseService{name: name, driver: driver, logger: a.Logger}
	a.container.Store(dbKey(name), svc)
	return a
}

// InjectDatabase registers an already-open *gorm.DB as the default database,
// bypassing config loading and Init. Use it to supply a connection the caller
// built directly — typically a shared in-memory database in tests. The host
// still closes it and includes it in health checks.
func (a *App) InjectDatabase(db *gorm.DB) *App {
	return a.InjectNamedDatabase(DefaultDatabase, db)
}

// InjectNamedDatabase registers an already-open *gorm.DB under name, bypassing
// config loading and Init.
func (a *App) InjectNamedDatabase(name string, db *gorm.DB) *App {
	if a.err != nil {
		return a
	}
	if db == nil {
		return a.fail(fmt.Errorf("database %q: nil *gorm.DB", name))
	}
	a.container.Store(dbKey(name), &databaseService{name: name, db: db, logger: a.Logger})
	return a
}
