# Database migrations

MicroJet's core does **not** ship a migration engine. The examples call GORM's
`AutoMigrate` for brevity, which is fine for prototyping and tests but is **not
recommended for production**: it never drops or renames columns, can't express
data backfills, and gives you no versioned, reviewable history of schema change.

## Opt-in module: `gormx/migrate`

For the common case there is a thin opt-in wrapper around
[goose](https://github.com/pressly/goose):
`github.com/hatami57/microjet/gormx/migrate`. It derives the goose dialect from
the gorm driver (Postgres, SQLite, MySQL), so embedded SQL migrations apply with
one call:

```go
import (
    "context"
    "embed"

    "github.com/hatami57/microjet/gormx"
    "github.com/hatami57/microjet/gormx/migrate"
    "github.com/hatami57/microjet/gormx/postgres"
    "github.com/hatami57/microjet/host"
    "github.com/hatami57/microjet/httpx"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

app := host.MustNew().
    WithModule(gormx.Module(postgres.Driver())).
    WithModule(httpx.Module()).
    Setup(func(a *host.App) error {
        return migrate.Up(context.Background(), gormx.Of(a), migrationsFS)
    }).
    Setup(registerRoutes)
```

`migrate.New` returns a `*Migrator` exposing `Up`, `Down`, and `Version` for
finer control. By default it reads from the `migrations` directory of the
provided `fs.FS`; override with `migrate.WithDir`.

If you prefer another engine, or need features goose doesn't cover, both
[golang-migrate](https://github.com/golang-migrate/migrate) and goose work
directly against the `*sql.DB` that GORM uses, as shown below.

## Where migrations run

Run migrations **before** the app starts serving — typically as a `host.Setup`
step in the fluent chain, or as a separate one-shot job/CLI in your deploy
pipeline. Retrieve the connection with `gormx.Of(app)` (and
`gormx.Of(app, name)` for [named databases](../README.md)).

```go
app := host.MustNew().
    WithModule(gormx.Module(postgres.Driver())).
    WithModule(httpx.Module()).
    Setup(func(a *host.App) error {
        return runMigrations(a) // apply pending migrations before serving
    }).
    Setup(registerRoutes)

app.MustRun()
```

A `Setup` step that returns an error short-circuits the chain and is surfaced by
`Run`/`MustRun`/`Err`, so a failed migration prevents the service from starting.

## golang-migrate (embedded SQL files)

Keep `.up.sql` / `.down.sql` files under `migrations/` and embed them:

```go
import (
    "embed"
    "github.com/golang-migrate/migrate/v4"
    "github.com/golang-migrate/migrate/v4/database/postgres"
    "github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func runMigrations(a *host.App) error {
    sqlDB, err := gormx.Of(a).DB()
    if err != nil {
        return err
    }
    driver, err := postgres.WithInstance(sqlDB, &postgres.Config{})
    if err != nil {
        return err
    }
    src, err := iofs.New(migrationsFS, "migrations")
    if err != nil {
        return err
    }
    m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
    if err != nil {
        return err
    }
    if err := m.Up(); err != nil && err != migrate.ErrNoChange {
        return err
    }
    return nil
}
```

## goose (embedded SQL files)

```go
import (
    "embed"
    "github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func runMigrations(a *host.App) error {
    sqlDB, err := gormx.Of(a).DB()
    if err != nil {
        return err
    }
    goose.SetBaseFS(migrationsFS)
    if err := goose.SetDialect("postgres"); err != nil {
        return err
    }
    return goose.Up(sqlDB, "migrations")
}
```

## Notes

- These tools manage their own version table; keep `AutoMigrate` out of
  production paths once you adopt one.
- For SQLite use the matching driver (`golang-migrate/.../database/sqlite` or
  `goose.SetDialect("sqlite3")`).
- For multiple databases registered via named modules
  (`gormx.Module(driver, name)`), run the appropriate migration set against
  each `gormx.Of(app, name)`.
- Run migrations from a single instance (or behind an advisory lock) to avoid
  concurrent apply during rolling deploys.
