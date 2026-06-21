// Command migrations demonstrates MicroJet's opt-in versioned SQL migrations
// (gormx/migrate, a thin goose wrapper). Unlike gormx AutoMigrate — which
// derives schema from structs — this applies hand-written, ordered .sql files
// embedded in the binary, giving you reversible Up/Down steps and a recorded
// schema version. The goose dialect is derived from the gorm driver, so the
// same code runs on Postgres, MySQL, or (here) SQLite.
//
// Run it with:
//
//	go run .
package main

import (
	"context"
	"embed"

	"github.com/hatami57/microjet/gormx"
	"github.com/hatami57/microjet/gormx/migrate"
	"github.com/hatami57/microjet/gormx/sqlite"
	"github.com/hatami57/microjet/host"
)

// Migration files are embedded so the binary is self-contained. We keep them in
// a "schema" directory and point migrate at it with WithDir (the default is
// "migrations").
//
//go:embed schema/*.sql
var migrationsFS embed.FS

func main() {
	// Bring up the database (config.toml points SQLite at an in-memory db), but
	// don't start serving — InitServices runs the connect phase and returns.
	app := host.MustNew().WithModule(gormx.Module(sqlite.Driver())).InitServices()
	if err := app.Err(); err != nil {
		panic(err)
	}
	defer app.Close()

	ctx := context.Background()
	db := gormx.Of(app)

	// Build a Migrator so we can inspect the version and roll back; for a plain
	// "apply everything" you can call
	// migrate.Up(ctx, db, migrationsFS, migrate.WithDir("schema")) directly from a
	// host Setup hook.
	m, err := migrate.New(db, migrationsFS, migrate.WithDir("schema"), migrate.WithLogger(app.Logger))
	if err != nil {
		panic(err)
	}

	v0, _ := m.Version(ctx)
	app.Logger.Info("starting version", "version", v0) // 0: nothing applied yet

	// Apply all pending migrations in order (00001, then 00002).
	if err := m.Up(ctx); err != nil {
		panic(err)
	}
	v1, _ := m.Version(ctx)
	app.Logger.Info("after Up", "version", v1) // 2

	// The schema now exists — insert and read back a row through the column the
	// second migration added.
	type Widget struct {
		ID    uint `gorm:"primaryKey"`
		Name  string
		Color string
	}
	db.Create(&Widget{Name: "gizmo", Color: "blue"})
	var w Widget
	db.First(&w)
	app.Logger.Info("inserted row", "name", w.Name, "color", w.Color)

	// Down rolls back exactly one migration (drops the color column).
	if err := m.Down(ctx); err != nil {
		panic(err)
	}
	v2, _ := m.Version(ctx)
	app.Logger.Info("after one Down", "version", v2) // 1
}
