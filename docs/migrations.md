# Database migrations

microjet does **not** ship a migration engine. The examples call GORM's
`AutoMigrate` for brevity, which is fine for prototyping and tests but is **not
recommended for production**: it never drops or renames columns, can't express
data backfills, and gives you no versioned, reviewable history of schema change.

For production use a dedicated migration tool. The two common choices in the Go
ecosystem are [golang-migrate](https://github.com/golang-migrate/migrate) and
[goose](https://github.com/pressly/goose). Both work with the same `*sql.DB`
that GORM uses, so they integrate cleanly with microjet.

## Where migrations run

Run migrations **before** the app starts serving — typically as a `host.Setup`
step in the fluent chain, or as a separate one-shot job/CLI in your deploy
pipeline. The `App` exposes the underlying connection via `app.DB()` (and
`app.NamedDB(name)` for [named databases](../README.md)).

```go
app := host.MustNew().
    WithDatabaseFromConfig().
    Setup(func(a *host.App) error {
        return runMigrations(a) // apply pending migrations before serving
    }).
    WithHTTPServer(registerRoutes)

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
    sqlDB, err := a.DB().DB()
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
    sqlDB, err := a.DB().DB()
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
- For multiple databases registered via `WithDatabasesFromConfig`, run the
  appropriate migration set against each `app.NamedDB(name)`.
- Run migrations from a single instance (or behind an advisory lock) to avoid
  concurrent apply during rolling deploys.
