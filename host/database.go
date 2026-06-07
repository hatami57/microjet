package host

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/gormx"
	"gorm.io/gorm"
)

// databaseService implements core.Configurable, core.Initer, core.Closer, and
// core.HealthChecker. It resolves its config section, hands the result to its
// Driver to open a connection, and manages that connection's lifecycle. A driver
// of type existingDB (via ExistingDB) returns a pre-built *gorm.DB and ignores
// the resolved config.
type databaseService struct {
	name    string
	section string // config section override; empty means derive from name
	driver  Driver
	config  gormx.Config
	logger  *slog.Logger
	db      *gorm.DB
}

func (d *databaseService) LoadConfig(l *core.ConfigLoader) error {
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
// opened during the host's init phase from the [database] config section.
// Pass a built-in driver (Postgres(), SQLite(), AutoDriver()), ExistingDB to
// inject a pre-built connection, or any custom Driver implementation.
func (a *App) WithDatabase(driver Driver, opts ...DBOption) *App {
	return a.WithNamedDatabase(DefaultDatabase, driver, opts...)
}

// WithNamedDatabase registers driver under name. Config is loaded from
// [database.<name>] unless overridden with Section. Use this to run several
// databases side by side, each retrievable via NamedDB(name).
func (a *App) WithNamedDatabase(name string, driver Driver, opts ...DBOption) *App {
	if a.err != nil {
		return a
	}
	if driver == nil {
		return a.fail(fmt.Errorf("database %q: nil driver", name))
	}
	svc := &databaseService{name: name, driver: driver, logger: a.Logger}
	for _, opt := range opts {
		opt(svc)
	}
	a.container.Store(dbKey(name), svc)
	return a
}

// WithDatabaseFromConfig registers a database whose engine is selected by the
// "driver" field of [database] (default) or [database.<name>] (named) at init
// time. Equivalent to WithDatabase(AutoDriver()) / WithNamedDatabase(name, AutoDriver()).
func (a *App) WithDatabaseFromConfig(name ...string) *App {
	return a.WithNamedDatabase(firstOrEmpty(name), AutoDriver())
}

// WithDatabasesFromConfig discovers all named databases defined as sub-tables
// under [database] (e.g. [database.analytics]) and registers each with
// AutoDriver. Connections are established in sorted name order at init time.
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
		a.WithNamedDatabase(n, AutoDriver())
	}
	return a
}

func firstOrEmpty(s []string) string {
	if len(s) > 0 {
		return s[0]
	}
	return ""
}
