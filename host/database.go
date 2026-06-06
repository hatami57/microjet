package host

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/gormx"
	"gorm.io/gorm"
)

// databaseService implements core.Configurable, core.Initer, and core.Closer.
// A preset driver (postgres/sqlite) overrides whatever driver the TOML specifies.
// Pre-injecting db (via WithDatabase/WithNamedDatabase) skips both LoadConfig
// and Init so tests can supply *gorm.DB directly.
type databaseService struct {
	name       string
	driver     string // overrides config.Driver when set
	config     gormx.Config
	logger     *slog.Logger
	db         *gorm.DB
	configured bool // true = config already populated, skip LoadConfig
}

func (d *databaseService) LoadConfig(l *core.ConfigLoader) error {
	if d.configured {
		return nil
	}
	key := "database"
	if d.name != "" {
		key = "database." + d.name
	}
	if err := l.UnmarshalKey(key, &d.config); err != nil {
		return err
	}
	if d.driver != "" {
		d.config.Driver = d.driver
	}
	return nil
}

func (d *databaseService) Init() error {
	if d.db != nil {
		return nil
	}
	db, err := connectDatabase(d)
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

// WithDatabase injects a pre-built *gorm.DB as the default database,
// bypassing config loading and Init. Intended for tests and custom setups.
func (a *App) WithDatabase(db *gorm.DB) *App {
	return a.WithNamedDatabase(DefaultDatabase, db)
}

// WithNamedDatabase injects a pre-built *gorm.DB under a specific name,
// bypassing config loading and Init.
func (a *App) WithNamedDatabase(name string, db *gorm.DB) *App {
	a.container.Store(dbKey(name), &databaseService{
		name:       name,
		db:         db,
		logger:     a.Logger,
		configured: true,
	})
	return a
}

// WithDatabaseFromConfig registers a database service that loads its config
// from [database] (default) or [database.<name>] (named) at Init time.
// Pass a name to register a named database; no args registers the default.
func (a *App) WithDatabaseFromConfig(name ...string) *App {
	if a.err != nil {
		return a
	}
	n := firstOrEmpty(name)
	a.container.Store(dbKey(n), &databaseService{name: n, logger: a.Logger})
	return a
}

// WithDatabasesFromConfig discovers all named databases defined as sub-tables
// under [database] (e.g. [database.analytics]) and registers each as a
// service. Connections are established in sorted name order at Init time.
func (a *App) WithDatabasesFromConfig() *App {
	if a.err != nil {
		return a
	}
	dbMap := a.configLoader.GetStringMap("database")
	names := make([]string, 0, len(dbMap))
	for name, val := range dbMap {
		if _, ok := val.(map[string]any); ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return a.fail(fmt.Errorf("database: no named databases found under [database.*]"))
	}
	slices.Sort(names)
	for _, n := range names {
		a.container.Store(dbKey(n), &databaseService{name: n, logger: a.Logger})
	}
	return a
}

// connectDatabase opens a GORM connection for the given service, dispatching on driver.
func connectDatabase(d *databaseService) (*gorm.DB, error) {
	switch strings.ToLower(strings.TrimSpace(d.config.Driver)) {
	case "postgres", "postgresql":
		return newPostgreSQL(d)
	case "sqlite", "sqlite3":
		return newSQLite(d)
	case "":
		return nil, fmt.Errorf("no driver configured (set driver in [database] config)")
	default:
		return nil, fmt.Errorf("unsupported driver %q", d.config.Driver)
	}
}

func firstOrEmpty(s []string) string {
	if len(s) > 0 {
		return s[0]
	}
	return ""
}
