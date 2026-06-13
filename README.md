# MicroJet

[![CI](https://github.com/hatami57/microjet/actions/workflows/ci.yml/badge.svg)](https://github.com/hatami57/microjet/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/hatami57/microjet.svg)](https://pkg.go.dev/github.com/hatami57/microjet)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A Go micro-framework for building production-grade microservices with minimal boilerplate.

```go
import "github.com/hatami57/microjet/host"
```

## Features

- **Application Orchestrator** — Fluent builder API (`MustNew().WithDatabase(postgres.Driver()).WithHTTPServer(...).MustRun()`) with deferred error handling, a dependency injection container, composable [modules](#modules) for tree-structured feature wiring, and managed graceful shutdown.
- **Structured Errors** — Typed error system with 6 categories (BadRequest, NotFound, Business, Unauthorized, Forbidden, Internal), builder-pattern enrichment, sentinel errors, `errors.As` extraction, and `errors.Is` matching by category (`errors.Is(err, core.ErrNotFound)`).
- **Configuration** — TOML-based config loading with environment variable overrides, local config merging, post-load hooks, and generic typed access to arbitrary sections. A missing config file is non-fatal — defaults plus env vars are enough to boot.
- **HTTP Server** — Gin-based server with built-in middleware (structured logging, error translation, recovery), health endpoint, Swagger UI (debug mode only), typed param/query/body binding, request validation that turns `binding`/`validate` tag failures into a 400 with per-field details (keyed by JSON name), multi-tenant support (with an optional TTL-cached tenant store), and graceful shutdown.
- **HTTP Client & Web Helpers** — `httpx.Client` for JSON calls to upstreams (default headers, per-request options, non-2xx → `core.Error`); `MergeParams` (query+form) and `WriteAutoPostForm` (self-submitting redirect form) for callback-style flows.
- **SQL / GORM** — `WithDatabase(driver)` with plug-in drivers (`gormx/postgres`, `gormx/sqlite` — pure-Go, no cgo). Generic `Table[T]` with CRUD, cursor-based pagination (by ID or created_at), transactions, batch inserts, and eager loading. `WithNamedDatabase` supports multiple databases side by side.
- **AWS Integration** — Unified S3 (single/concurrent download, upload), SQS (send JSON messages), and DynamoDB client initialization.
- **NATS Messaging** — Pub/sub with raw-byte delivery; pair with `types.Message` for structured JSON envelopes and graceful drain. `WithSubscriber` ties subscriptions to the app lifecycle (subscribe on start, drain on shutdown); `messaging.HandleJSON` / `HandleEnvelope` give typed handlers (`func(ctx, T) error`) with automatic decoding, and `WithQueueGroup` load-balances a subject across replicas.
- **Transactional Outbox** — `outbox.Enqueue`/`EnqueueJSON` record an event in the same DB transaction as your domain write; `WithOutbox()` migrates the table and runs a periodic relay that publishes pending events to the broker with at-least-once delivery, so events are never lost on a crash between commit and publish.
- **Distributed Tracing** — Opt-in OpenTelemetry via `WithTracing()`: the `otelx` module installs an OTLP exporter and the W3C propagator, and the instrumented layers — HTTP server and client, GORM, NATS — emit and propagate spans automatically (no-ops while tracing is off). Request logs carry `trace_id` for log/trace correlation.
- **Money Type** — Currency-aware decimal arithmetic (`Add`, `Sub`, `Multiply`) with currency validation, plus integer minor-unit conversion (`FromMinorUnits`/`MinorUnits`) with a zero/two/three-decimal currency registry.
- **Time Utilities** — `TimeProvider` interface for testability, sortable timestamp formats.
- **Type Converters** — Generic JSON, struct-to-map, and pointer coalescing utilities.
- **Logging** — Structured `log/slog` with configurable level and format (text/JSON).

## Packages

| Package | Import | Description |
|---|---|---|
| `host` | `github.com/hatami57/microjet/host` | Application orchestrator, DI container, lifecycle |
| `core` | `github.com/hatami57/microjet/core` | Errors, config loading, logging, time |
| `httpx` | `github.com/hatami57/microjet/httpx` | Gin HTTP server, middleware, request helpers, JSON client, web helpers |
| `gormx` | `github.com/hatami57/microjet/gormx` | Generic GORM CRUD + cursor pagination (works with any `*gorm.DB`, incl. SQLite) |
| `messaging` | `github.com/hatami57/microjet/messaging` | NATS pub/sub client (context + headers) |
| `cache` | `github.com/hatami57/microjet/cache` | Cache interface with Redis and in-memory implementations |
| `otelx` | `github.com/hatami57/microjet/otelx` | OpenTelemetry tracing setup (OTLP exporter, W3C propagation) |
| `gormx/migrate` | `github.com/hatami57/microjet/gormx/migrate` | Opt-in versioned SQL migrations (goose wrapper) |
| `outbox` | `github.com/hatami57/microjet/outbox` | Transactional outbox: enqueue events in a DB tx, relay to the broker |
| `aws` | `github.com/hatami57/microjet/aws` | S3, SQS, DynamoDB clients |
| `types` | `github.com/hatami57/microjet/types` | Message envelope, pagination types |
| `types/money` | `github.com/hatami57/microjet/types/money` | Currency-aware decimal money |
| `utils` | `github.com/hatami57/microjet/utils` | JSON, converters, env, disk |

## Installation

```bash
go get github.com/hatami57/microjet/host
```

## Quick Start

### Minimal Application

```go
package main

import "github.com/hatami57/microjet/host"

func main() {
 app := host.MustNew()
 defer app.Close()

 app.Logger.Info("started")
 host.WaitForExitSignal()
}
```

`host.New()` returns `(*App, error)` so it is safe to use in libraries and
tests; `host.MustNew()` is the panic-on-error convenience for `main()`.

### With Custom App Config

```go
type MyConfig struct {
 ServiceName string `mapstructure:"serviceName"`
 MaxWorkers  int    `mapstructure:"maxWorkers"`
}

func (c *MyConfig) LoadConfig(l *core.ConfigLoader) error {
 return l.UnmarshalKey("myapp", c)
}

func main() {
 app := host.MustNew(host.WithEnvPrefix("MYAPP"))
 defer app.Close()

 var cfg MyConfig
 app.LoadConfig(&cfg)
 app.Logger.Info("started", "service", cfg.ServiceName)
 host.WaitForExitSignal()
}
```

### Full HTTP + PostgreSQL

```go
package main

import (
 "net/http"

 "github.com/gin-gonic/gin"
 "github.com/hatami57/microjet/gormx"
 "github.com/hatami57/microjet/gormx/postgres"
 "github.com/hatami57/microjet/host"
 "github.com/hatami57/microjet/httpx"
)

type User struct {
 ID    uint   `gorm:"primaryKey"`
 Name  string `gorm:"not null"`
 Email string `gorm:"unique;not null"`
}

func main() {
 app := host.MustNew()

 app.WithDatabase(postgres.Driver()).
  Setup(func(a *host.App) error {
   return a.DB().AutoMigrate(&User{})
  }).
  WithHTTPServer(func(a *host.App) error {
   users := gormx.NewTable[User](a.DB())
   a.HTTPServer.Router.GET("/users", func(c *gin.Context) {
    req := gormx.NewPageRequest[User, uint](httpx.PagedRequest(c), "id", func(u User) uint { return u.ID })
    items, _ := users.List(c.Request.Context(), req)
    c.JSON(http.StatusOK, items)
   })
   return nil
  }).
  MustRun() // inits services, starts HTTP, blocks until signal, then shuts down
}
```

`Run()` / `MustRun()` manage the full lifecycle: they initialize registered
services, start the HTTP server, block until SIGINT/SIGTERM (or a fatal server
error), and then perform a graceful shutdown — so you don't need a separate
`defer app.Close()`. Use `Setup(...)` for one-off startup steps like migrations.
For manual control, use `app.StartHTTP()` + `host.WaitForExitSignal()` with
`defer app.Close()` instead.

### Database drivers

`WithDatabase(driver)` reads the `[database]` config section, opens the
connection during init, and registers it as the default database (retrieve with
`app.DB()`). Two built-in drivers are available as separate opt-in modules:

```go
import "github.com/hatami57/microjet/gormx/postgres"
app.WithDatabase(postgres.Driver())

import "github.com/hatami57/microjet/gormx/sqlite"
app.WithDatabase(sqlite.Driver()) // pure-Go, no cgo; set database.name = ":memory:" for in-memory
```

To run multiple databases side by side, use `WithNamedDatabase(name, driver)`;
retrieve each with `app.NamedDB(name)`. Each named database reads its config
from `[database.<name>]`.

### Cached tenant lookups

`middleware.Tenant(store)` resolves the tenant on every request from the
`X-Tenant-ID` header or `tenantId` query param. To avoid a per-request store
hit, wrap any `TenantStore` in `middleware.NewCachedTenantStore` — it caches
both hits and "not found" results for the given TTL and exposes `Invalidate(id)`
for when a tenant changes:

```go
cached := middleware.NewCachedTenantStore(dbStore, 5*time.Minute)
router.Use(middleware.Tenant(cached))
```

## Configuration

Create `config.toml` (auto-discovered from working directory, `./config/`, or exe directory). An optional `config.local.toml` in the same locations is merged on top — useful for local overrides that should not be committed.

```toml
[app]
namespace = "MyApp"
environment = "production"
name = "My Service"
version = "1.0.0"
debug = false

[server]
host = "0.0.0.0"
port = 8080

[database]
driver = "postgres"
host = "localhost"
port = 5432
user = "myuser"
password = "mypassword"
name = "mydb"
sslMode = "disable"

[messaging]
url = "nats://localhost:4222"
source = "my-service"
version = 1

[aws]
accessKey = "your-access-key"
secretKey = "your-secret-key"
region = "us-east-1"

[log]
level = "info"
format = "json"
```

Override via env vars: `APP_DATABASE_HOST=prodhost`, `APP_SERVER_PORT=443` (prefix defaults to `APP`, configurable via `host.WithEnvPrefix`).

### Extra Config

Implement `core.Configurable` on your config struct and pass it to `app.LoadConfig`:

```go
type MyExtra struct {
 WorkerCount int    `mapstructure:"workerCount"`
 QueueName   string `mapstructure:"queueName"`
}

func (c *MyExtra) LoadConfig(l *core.ConfigLoader) error {
 return l.UnmarshalKey("myapp", c)
}

// At startup:
var extra MyExtra
app.LoadConfig(&extra)
```

## Error Handling

```go
import "github.com/hatami57/microjet/core"

// Builder-pattern enrichment
err := core.NewNotFoundError("User", "user not found").
    WithCode(1001).
    WithInner(fmt.Errorf("db: %w", originalErr))

// Sentinel errors
if err != nil {
    return core.ErrBadRequest.WithSubject("email")
}

// Check error type
switch {
case core.IsNotFoundError(err):
    // handle 404
case core.IsBadRequestError(err):
    // handle 400
}
```

## HTTP Helpers

```go
import "github.com/hatami57/microjet/httpx"

// Typed param/query extraction
id, _ := httpx.FindUUIDParam(c, "id")
name, _ := httpx.FindQuery(c, "name")
pageSize, _ := httpx.FindInt64Query(c, "pageSize")

// JSON body binding
body, _ := httpx.Body[CreateUserRequest](c)

// Pagination request from query string
pagedReq := httpx.PagedRequest(c)
```

## Pagination

```go
import (
    "github.com/hatami57/microjet/httpx"
    "github.com/hatami57/microjet/gormx"
)

// Cursor-based pagination by ID
req := gormx.NewPageRequest[User, uint](httpx.PagedRequest(c), "id", func(u User) uint { return u.ID }).
    SetWhere("name ILIKE ?", "%john%")

result, _ := userTable.List(ctx, req)
for _, user := range result.Items {
    // ...
}
// result.NextPageToken is base64-encoded cursor for the next page
```

## Money

```go
import "github.com/hatami57/microjet/types/money"

price := money.Money{Value: decimal.NewFromFloat(10.50), CurrencyCode: "USD"}
tax := price.MultiplyInt64(2) // 21.00 USD
total, _ := price.Add(&tax)   // 31.50 USD
```

## Dependency Injection

```go
type UserService struct{}

func (s *UserService) Init(app *host.App) error {
    app.Logger.Info("UserService initialized")
    return nil
}

app := host.MustNew()
host.ProvideService(app, &UserService{})

// Later — returns (T, bool):
svc, ok := host.ResolveService[*UserService](app)

// Or panic if not registered:
svc := host.MustResolveService[*UserService](app)
```

## Modules

A **Module** bundles a slice of functionality — its services, routes, config, and
workers — behind one `Register` hook, and may install further modules, forming a
tree. This is how you compose a service out of self-contained features (and how a
feature pulls in the features it depends on) without a giant `main()`.

```go
type Module interface {
    Register(app *host.App) error
}
```

Install modules with the fluent chain; `WithModule` runs the module's `Register`,
and a module's `Register` installs its children the same way:

```go
type EmailModule struct{}

func (EmailModule) Register(app *host.App) error {
    host.ProvideService(app, &EmailSender{}) // a managed service
    return nil
}

type UsersModule struct{}

func (UsersModule) Register(app *host.App) error {
    app.WithModule(EmailModule{}) // child module
    host.ProvideService(app, &UserService{})
    return app.Setup(func(a *host.App) error {
        registerUserRoutes(a)
        return nil
    }).Err()
}

func main() {
    host.MustNew().
        WithHTTPServer().
        WithModule(UsersModule{}). // brings EmailModule with it
        MustRun()
}
```

Key behaviors:

- **Recursive** — a module's `Register` can install any number of child modules.
- **Deduplicated** — struct modules install once per type, so a shared module
  imported by several parents (the "diamond" case) is registered exactly once.
  Implement `KeyedModule` (`ModuleKey() string`) to install the same type more
  than once with different config; `host.ModuleFunc` wraps a plain function for
  one-off modules and is never deduplicated.
- **Register provides, `Init` wires** — `Register` should only *provide* services
  and *import* child modules, never resolve dependencies (siblings and children
  may register afterwards). Resolve dependencies in each service's `Init(app)` /
  `Start(app)`, which runs after every module has registered, so registration
  order doesn't matter. Provided services join the normal lifecycle (config →
  init → start → close) regardless of how deeply they were nested.

See [`examples/modules`](examples/modules) for a runnable three-level tree.

## Graceful Shutdown

`app.Close()` drains messaging, stops the HTTP server, closes DB connections, and calls all registered `ServiceCloser` implementations — all running concurrently with a `sync.WaitGroup`. It is idempotent (guarded by `sync.Once`), so it is safe to call it via both `Run()`/`MustRun()` and a `defer app.Close()`.

## Examples

Runnable example services live in [`examples/`](examples/):

- [`examples/minimal`](examples/minimal) — smallest possible app.
- [`examples/http-postgres`](examples/http-postgres) — HTTP CRUD backed by PostgreSQL.
- [`examples/sqlite`](examples/sqlite) — HTTP CRUD backed by SQLite; runs with no external database.
- [`examples/features`](examples/features) — middleware (CORS, rate limit, JWT), request-scoped logging, the cache, and the JSON client with retries.
- [`examples/modules`](examples/modules) — composable modules: a three-level dependency tree with shared-module deduplication.

For database migrations in production, see [`docs/migrations.md`](docs/migrations.md).

## Architecture

```
utils  (JSON, converters, env)
  |
  +-- types  (Message, PagedResult, money)
  |
  +-- aws  (S3, SQS, DynamoDB)
  |
core  (errors, config, logging, time)
  |
  +-- gormx    (Table[T], pagination, Service lifecycle)
  +-- messaging  (NATS client)
  +-- httpx  (Gin server, middleware, helpers)
        |
        +-- host  (orchestrator, DI, lifecycle)
              imports: aws, core, httpx, messaging, gormx, types, utils
```

## License

MIT
