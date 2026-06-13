// Package migrate runs versioned SQL migrations against a gormx-managed
// database using goose. It is an opt-in module so the core framework stays free
// of a migration-tool dependency: import it only where you need migrations.
//
// Typical use is from a host Setup handler, which runs after the database is
// connected but before the server starts serving:
//
//	//go:embed migrations/*.sql
//	var migrationsFS embed.FS
//
//	app.WithDatabase(postgres.Driver()).
//	    Setup(func(a *host.App) error {
//	        return migrate.Up(context.Background(), a.DB(), migrationsFS)
//	    })
package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
)

// DefaultDir is the directory within the provided fs.FS that holds migration
// files. It matches the common `//go:embed migrations/*.sql` layout.
const DefaultDir = "migrations"

// Migrator applies and inspects migrations for a single database. Build one with
// New and reuse it; it is safe for sequential use.
type Migrator struct {
	provider *goose.Provider
	logger   *slog.Logger
}

type config struct {
	dir    string
	logger *slog.Logger
}

// Option configures a Migrator.
type Option func(*config)

// WithDir overrides the directory within the fs.FS that holds migration files
// (default "migrations"). Pass "." when the fs.FS is already rooted at the
// migrations directory.
func WithDir(dir string) Option {
	return func(c *config) {
		if dir != "" {
			c.dir = dir
		}
	}
}

// WithLogger sets the logger used to report applied/rolled-back migrations.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.logger = l
		}
	}
}

// New builds a Migrator for db, reading migration files from fsys. The goose
// dialect is derived from the gorm driver, so the same call works for Postgres,
// SQLite, and MySQL.
func New(db *gorm.DB, fsys fs.FS, opts ...Option) (*Migrator, error) {
	cfg := config{dir: DefaultDir, logger: slog.Default()}
	for _, o := range opts {
		o(&cfg)
	}
	if db == nil {
		return nil, fmt.Errorf("migrate: nil *gorm.DB")
	}

	dialect, err := dialectFor(db)
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("migrate: resolving *sql.DB: %w", err)
	}

	root := fsys
	if cfg.dir != "" && cfg.dir != "." {
		root, err = fs.Sub(fsys, cfg.dir)
		if err != nil {
			return nil, fmt.Errorf("migrate: opening migrations dir %q: %w", cfg.dir, err)
		}
	}

	provider, err := goose.NewProvider(dialect, sqlDB, root)
	if err != nil {
		return nil, fmt.Errorf("migrate: building goose provider: %w", err)
	}
	return &Migrator{provider: provider, logger: cfg.logger}, nil
}

// Up applies every pending migration in order. It is a no-op when the database
// is already current.
func (m *Migrator) Up(ctx context.Context) error {
	results, err := m.provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("migrate up: %w", err)
	}
	for _, r := range results {
		m.logger.Info("migration applied", "version", r.Source.Version, "file", r.Source.Path, "duration", r.Duration)
	}
	if len(results) == 0 {
		m.logger.Debug("no pending migrations")
	}
	return nil
}

// Down rolls back the most recently applied migration.
func (m *Migrator) Down(ctx context.Context) error {
	result, err := m.provider.Down(ctx)
	if err != nil {
		return fmt.Errorf("migrate down: %w", err)
	}
	m.logger.Info("migration rolled back", "version", result.Source.Version, "file", result.Source.Path)
	return nil
}

// Version returns the current schema version (0 when no migration has run).
func (m *Migrator) Version(ctx context.Context) (int64, error) {
	v, err := m.provider.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("migrate version: %w", err)
	}
	return v, nil
}

// Up is a convenience wrapper that builds a Migrator and applies all pending
// migrations. Use it directly from a host Setup handler.
func Up(ctx context.Context, db *gorm.DB, fsys fs.FS, opts ...Option) error {
	m, err := New(db, fsys, opts...)
	if err != nil {
		return err
	}
	return m.Up(ctx)
}

// dialectFor maps a gorm driver name to the corresponding goose dialect.
func dialectFor(db *gorm.DB) (goose.Dialect, error) {
	switch name := db.Dialector.Name(); name {
	case "postgres":
		return goose.DialectPostgres, nil
	case "sqlite", "sqlite3":
		return goose.DialectSQLite3, nil
	case "mysql":
		return goose.DialectMySQL, nil
	default:
		return "", fmt.Errorf("migrate: unsupported dialect %q", name)
	}
}
