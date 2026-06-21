package gormx

import (
	"fmt"

	"github.com/hatami57/microjet/host"
	"gorm.io/gorm"
)

// DefaultDatabase names the primary database. It is accepted interchangeably with
// the empty name, so gormx.Module(d) and gormx.Module(d, gormx.DefaultDatabase)
// register the same connection and gormx.Of(app) / gormx.Of(app, "default") both
// resolve it.
const DefaultDatabase = "default"

// canonicalName folds the default database's two spellings ("" and "default")
// onto the empty name used as the container key, so the primary database is a
// single instance however it is addressed.
func canonicalName(name string) string {
	if name == DefaultDatabase {
		return ""
	}
	return name
}

// dbSection returns the config section for a database. The default database maps
// to [database]; a named one to [database.<name>].
func dbSection(name string) string {
	if canonicalName(name) == "" {
		return "database"
	}
	return "database." + name
}

// Module registers driver as a database, opening the connection during the
// host's init phase from the [database] config section (or [database.<name>] when
// named). Pass a built-in driver (postgres.Driver(), sqlite.Driver()) or any
// custom Driver; supply an already-open *gorm.DB with Inject instead.
//
//	host.MustNew().WithModule(gormx.Module(sqlite.Driver())).MustRun()
//
// Pass an optional name to run several databases side by side, each retrieved
// with gormx.Of(app, name). Reach the default with gormx.Of(app).
func Module(driver Driver, name ...string) host.Module {
	n := first(name)
	return host.KeyedModuleFunc(moduleKey("gormx.DB", canonicalName(n)), func(app *host.App) error {
		if driver == nil {
			return fmt.Errorf("database %q: nil driver", n)
		}
		svc := NewService(displayName(n), dbSection(n), driver)
		svc.SetLogger(app.Logger)
		host.ProvideService(app, svc, canonicalName(n))
		return nil
	})
}

// Inject registers an already-open *gorm.DB as a database, bypassing config
// loading and Init. Use it to supply a connection the caller built directly —
// typically a shared in-memory database in tests. The host still closes it and
// includes it in health checks. Pass an optional name for a named database.
func Inject(db *gorm.DB, name ...string) host.Module {
	n := first(name)
	return host.KeyedModuleFunc(moduleKey("gormx.DB", canonicalName(n)), func(app *host.App) error {
		if db == nil {
			return fmt.Errorf("database %q: nil *gorm.DB", n)
		}
		svc := NewServiceFromDB(displayName(n), db)
		svc.SetLogger(app.Logger)
		host.ProvideService(app, svc, canonicalName(n))
		return nil
	})
}

// CloseOrder closes the database late (as a backend), after the edges that use it
// (HTTP servers, subscribers) have drained.
func (s *Service) CloseOrder() int { return host.CloseBackend }

// Of returns the database connection registered under the optional name, or nil
// if none is registered.
func Of(app *host.App, name ...string) *gorm.DB {
	svc, ok := host.ResolveService[*Service](app, canonicalName(first(name)))
	if !ok {
		return nil
	}
	return svc.DB()
}

// first returns the first name or "" — the default database.
func first(name []string) string {
	if len(name) > 0 {
		return name[0]
	}
	return ""
}

// moduleKey builds a module dedup key for a slot and instance name.
func moduleKey(slot, name string) string {
	if name == "" {
		return slot
	}
	return slot + "[" + name + "]"
}

// displayName is the human-readable name used in logs; the default database's
// empty name shows as "default".
func displayName(name string) string {
	if name == "" {
		return DefaultDatabase
	}
	return name
}
