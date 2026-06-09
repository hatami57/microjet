package host

import (
	"fmt"

	"github.com/hatami57/microjet/gormx"
	"gorm.io/gorm"
)

// DefaultDatabase is the key used for the primary database registered via WithDatabase or InjectDatabase.
const DefaultDatabase = "default"

// dbKey returns the sync.Map key for a named database.
// An empty name maps to "db:default".
func dbKey(name string) string {
	if name == "" || name == DefaultDatabase {
		return "db:" + DefaultDatabase
	}
	return "db:" + name
}

// dbSection returns the config key for a named database.
// The default database maps to [database]; a named one to [database.<name>].
func dbSection(name string) string {
	if name == "" || name == DefaultDatabase {
		return "database"
	}
	return "database." + name
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
	svc, ok := v.(*gormx.Service)
	if !ok {
		return nil
	}
	return svc.DB()
}

// WithDatabase registers driver as the default database. The connection is
// opened during the host's init phase from the [database] config section. Pass a
// built-in driver (gormx.Postgres(), gormx.SQLite()) or any custom gormx.Driver.
// To supply an already-open *gorm.DB instead, use InjectDatabase.
func (a *App) WithDatabase(driver gormx.Driver) *App {
	return a.WithNamedDatabase(DefaultDatabase, driver)
}

// WithNamedDatabase registers driver under name. Config is loaded from
// [database.<name>] unless name is the default. Use this to run several
// databases side by side, each retrievable via NamedDB(name).
func (a *App) WithNamedDatabase(name string, driver gormx.Driver) *App {
	if a.err != nil {
		return a
	}
	if driver == nil {
		return a.fail(fmt.Errorf("database %q: nil driver", name))
	}
	svc := gormx.NewService(name, dbSection(name), driver)
	svc.SetLogger(a.Logger)
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
	svc := gormx.NewServiceFromDB(name, db)
	svc.SetLogger(a.Logger)
	a.container.Store(dbKey(name), svc)
	return a
}
