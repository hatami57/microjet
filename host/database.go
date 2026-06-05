package host

import (
	"fmt"
	"slices"
	"strings"

	"github.com/hatami57/microjet/core"
	"gorm.io/gorm"
)

// connectDatabase opens a GORM connection for a single database config,
// dispatching on its driver. Shared by the single- and multi-database helpers.
// Supported drivers: "postgres"/"postgresql" and "sqlite"/"sqlite3".
func (a *App) connectDatabase(dbCfg *core.DatabaseConfig) (*gorm.DB, error) {
	if dbCfg == nil {
		return nil, fmt.Errorf("nil database config")
	}
	switch strings.ToLower(strings.TrimSpace(dbCfg.Driver)) {
	case "postgres", "postgresql":
		return newPostgreSQL(a, dbCfg)
	case "sqlite", "sqlite3":
		return newSQLite(a, dbCfg)
	case "":
		return nil, fmt.Errorf("no driver configured (set driver)")
	default:
		return nil, fmt.Errorf("unsupported driver %q", dbCfg.Driver)
	}
}

// WithDatabasesFromConfig connects every database defined under the
// [databases.<name>] config tables and registers each under its name (retrieve
// later with NamedDB). The single [database] section is independent; use
// WithDatabaseFromConfig for that default connection. Connections are made in
// sorted name order for determinism. Errors are deferred to Run/MustRun/Err.
func (a *App) WithDatabasesFromConfig() *App {
	if a.err != nil {
		return a
	}
	if len(a.Config.Databases) == 0 {
		return a.fail(fmt.Errorf("database: no [databases] config section"))
	}
	names := make([]string, 0, len(a.Config.Databases))
	for name := range a.Config.Databases {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		db, err := a.connectDatabase(a.Config.Databases[name])
		if err != nil {
			return a.fail(fmt.Errorf("database %q: %w", name, err))
		}
		a.WithNamedDatabase(name, db)
	}
	return a
}
