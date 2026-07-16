# MicroJet

[![CI](https://github.com/hatami57/microjet/actions/workflows/ci.yml/badge.svg)](https://github.com/hatami57/microjet/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/hatami57/microjet.svg)](https://pkg.go.dev/github.com/hatami57/microjet)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A Go micro-framework for building production-grade microservices with minimal boilerplate.

```go
import "github.com/hatami57/microjet/host"
```

## Features

- **Application Orchestrator** — Fluent builder API (`MustNew().WithModules(gormx.Module(postgres.Driver()), httpx.Module()).MustRun()`) with deferred error handling, a dependency injection container, composable [modules](#modules) for tree-structured feature wiring, and managed graceful shutdown. Every capability — database, HTTP, AWS, messaging, cache, tracing, outbox — is installed the same way, as a `host.Module`, so the `host` runtime itself depends on none of them: you pull in only the satellite modules you actually use.
- **Structured Errors** — Typed error system with 6 categories (BadRequest, NotFound, Business, Unauthorized, Forbidden, Internal), builder-pattern enrichment, sentinel errors, `errors.As` extraction, and `errors.Is` matching by category (`errors.Is(err, errorx.ErrNotFound)`).
- **Configuration** — TOML-based config loading with environment variable overrides, local config merging, post-load hooks, and generic typed access to arbitrary sections. A missing config file is non-fatal — defaults plus env vars are enough to boot.
- **HTTP Server** — Gin-based server with built-in middleware (structured logging, error translation, recovery), health endpoint, Swagger UI and `/debug/pprof` profiling (debug mode only), typed param/query/body binding, request validation that turns `binding`/`validate` tag failures into a 400 with per-field details (keyed by JSON name), multi-tenant support (with an optional TTL-cached tenant store), configurable timeouts and TLS, opt-in hardening middleware (security headers, body-size limit, per-request timeout), and Kubernetes-aware graceful shutdown (readiness flips to draining before the pod is torn down).
- **HTTP Client & Web Helpers** — `httpx.Client` for JSON calls to upstreams (default headers, per-request options, non-2xx → `errorx.Error`), with optional retries (`WithRetry`) and a circuit breaker (`WithCircuitBreaker`) that fails fast when an upstream is down; `MergeParams` (query+form) and `WriteAutoPostForm` (self-submitting redirect form) for callback-style flows.
- **SQL / GORM** — `gormx.Module(driver)` with plug-in drivers (`gormx/postgres`, `gormx/sqlite` — pure-Go, no cgo). Generic `Table[T]` with CRUD (incl. `UpdateMap`/`UpdateColumn(s)` partial updates and `Raw`/`Exec` SQL escape hatches), a chainable query builder (`Where`/`WhereIf`/`Order`/`Limit`/`Offset`/`Select`/`Omit`/`Joins`/`Group`/`Having`/`Distinct`/`Unscoped`/`LockForUpdate`), single-row getters (`First`/`Last`/`Take`/`Get`/`Exists`), collectors (`Pluck`/`PluckDistinct`), aggregates (`Count`/`Sum`/`Avg`/`Max`/`Min`/`Aggregate`), struct or map projections (`Project`/`ProjectFirst`), cursor- and offset-based pagination, transactions, batch inserts and batched reads (`FindInBatches`), and eager loading. Portable constraint-error classifiers (`IsDuplicateKey`/`IsForeignKeyViolation`/`IsCheckConstraintViolation`/`IsRecordNotFound`) map driver-specific violations without importing gorm or sniffing SQLSTATE codes. `gormx.Module(driver, name)` supports multiple databases side by side.
- **AWS Integration** — Unified S3 (single/concurrent download, upload), SQS (send JSON messages), and DynamoDB client initialization.
- **NATS Messaging** — Pub/sub with raw-byte delivery; pair with `types.Message` for structured JSON envelopes and graceful drain. `messaging.Subscribe` ties subscriptions to the app lifecycle (subscribe on start, drain on shutdown); `messaging.HandleJSON` / `HandleEnvelope` give typed handlers (`func(ctx, T) error`) with automatic decoding, and `messaging.WithQueueGroup` load-balances a subject across replicas.
- **Transactional Outbox** — `outbox.Of(app).EnqueueJSON(ctx, ...)` records an event in the same gormx transaction (`RunTx`) as your domain write, capturing the caller's trace/correlation context so the relayed event keeps its lineage; `outbox.Module()` migrates the table and runs a relay that publishes pending events to the broker with at-least-once delivery (draining on an interval and promptly after each enqueue), so events are never lost on a crash between commit and publish. Tune durability with `MaxAttempts` (quarantine poison messages) and `Retention` (prune long-published rows).
- **Idempotency** — `middleware.Idempotency` replays the stored response for a repeated non-safe request carrying the same `Idempotency-Key`, so client retries don't act twice. Keys are scoped by method+route; backed by any store satisfying a small Get/Set interface (the app cache fits directly).
- **Distributed Tracing** — Opt-in OpenTelemetry via `otelx.Module()`: it installs an OTLP exporter and the W3C propagator, and the instrumented layers — HTTP server and client, GORM, NATS — emit and propagate spans automatically (no-ops while tracing is off). Request logs carry `trace_id` for log/trace correlation.
- **Money Type** — Currency-aware decimal arithmetic (`Add`, `Sub`, `Multiply`) with currency validation, plus integer minor-unit conversion (`FromMinorUnits`/`MinorUnits`) with a zero/two/three-decimal currency registry.
- **Time Utilities** — `TimeProvider` interface for testability, sortable timestamp formats.
- **Type Converters** — Generic JSON, struct-to-map, and pointer coalescing utilities.
- **Logging** — Structured `log/slog` with configurable level and format (text/JSON).

## Packages

| Package | Import | Description |
|---|---|---|
| `host` | `github.com/hatami57/microjet/host` | Application orchestrator, DI container, lifecycle |
| `core` | `github.com/hatami57/microjet/core` | Time, correlation, lifecycle interfaces; with subpackages `errorx` (typed errors), `logx` (slog setup), `configx` (config loading), plus `jsonx`, `utils`, `types`, `tenant`, `version` |
| `httpx` | `github.com/hatami57/microjet/httpx` | Gin HTTP server, middleware, request helpers, JSON client, web helpers |
| `gormx` | `github.com/hatami57/microjet/gormx` | Generic GORM CRUD + cursor & offset pagination (works with any `*gorm.DB`, incl. SQLite) |
| `messaging` | `github.com/hatami57/microjet/messaging` | NATS pub/sub client (context + headers) |
| `cache` | `github.com/hatami57/microjet/cache` | Cache interface with Redis and in-memory implementations |
| `otelx` | `github.com/hatami57/microjet/otelx` | OpenTelemetry tracing setup (OTLP exporter, W3C propagation) |
| `gormx/migrate` | `github.com/hatami57/microjet/gormx/migrate` | Opt-in versioned SQL migrations (goose wrapper) |
| `outbox` | `github.com/hatami57/microjet/outbox` | Transactional outbox: enqueue events in a DB tx, relay to the broker |
| `testx` | `github.com/hatami57/microjet/testx` | Test helpers: in-memory app builder, fake broker, HTTP request helpers |
| `aws` | `github.com/hatami57/microjet/aws` | S3, SQS, DynamoDB clients |
| `core/types` | `github.com/hatami57/microjet/core/types` | Message envelope, pagination types |
| `core/types/money` | `github.com/hatami57/microjet/core/types/money` | Currency-aware decimal money |
| `core/utils` | `github.com/hatami57/microjet/core/utils` | JSON, converters, env, disk |

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
import "github.com/hatami57/microjet/core/configx"

type MyConfig struct {
 ServiceName string `mapstructure:"serviceName"`
 MaxWorkers  int    `mapstructure:"maxWorkers"`
}

func (c *MyConfig) ReadConfig(l configx.Reader) error {
 return l.Read("myapp", c)
}

func main() {
 app := host.MustNew(host.WithEnvPrefix("MYAPP"))
 defer app.Close()

 var cfg MyConfig
 app.Configure(&cfg)
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

 app.WithModule(gormx.Module(postgres.Driver())).
  WithModule(httpx.Module()).
  Setup(func(a *host.App) error {
   return gormx.Of(a).AutoMigrate(&User{})
  }).
  Setup(func(a *host.App) error {
   users := gormx.NewTable[User](gormx.Of(a))
   httpx.Of(a).Router.GET("/users", func(c *gin.Context) {
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

To embed an App in a process that already owns cancellation — a monolith, CLI,
test, or supervisor — use the explicit runtime API instead of `Run()`. `Start`
brings the app up without blocking; `Wait` blocks until the app begins stopping
(the context is cancelled, a service's background loop exits, or `Shutdown` is
called); `Shutdown` performs the same graceful teardown as `Run`, bounded by the
context you pass:

```go
import (
 "context"

 "github.com/hatami57/microjet/host"
)

func run(ctx context.Context, app *host.App) error {
 if err := app.Start(ctx); err != nil {
  return err
 }
 defer app.Shutdown(context.Background())
 return app.Wait() // returns the fatal service error, if any
}
```

`Run()` is itself implemented on top of `Start`/`Wait`/`Shutdown`, adding only
signal handling. For lower-level manual control, `app.InitServices()` +
`host.WaitForExitSignal()` with `defer app.Close()` also works.

### Database drivers

`gormx.Module(driver)` reads the `[database]` config section, opens the
connection during init, and registers it as the default database (retrieve with
`gormx.Of(app)`). Two built-in drivers are available as separate opt-in modules:

```go
import "github.com/hatami57/microjet/gormx"
import "github.com/hatami57/microjet/gormx/postgres"
app.WithModule(gormx.Module(postgres.Driver()))

import "github.com/hatami57/microjet/gormx/sqlite"
app.WithModule(gormx.Module(sqlite.Driver())) // pure-Go, no cgo; set database.name = ":memory:" for in-memory
```

To run multiple databases side by side, pass a name to the same module:
`gormx.Module(driver, "analytics")`; retrieve each with
`gormx.Of(app, "analytics")`. Each named database reads its config from
`[database.<name>]`.

### Cached tenant lookups

`middleware.Tenant(store)` resolves the tenant on every request from the
`X-Tenant-ID` header or `tenantId` query param. To avoid a per-request store
hit, wrap any `tenant.Store` in `tenant.NewCachedStore` — it caches both hits
and "not found" results for the given TTL and exposes `Invalidate(id)` to drop
a single entry (and `Clear()` to flush them all) when a tenant changes:

```go
import "github.com/hatami57/microjet/core/tenant"

cached := tenant.NewCachedStore(dbStore, 5*time.Minute)
router.Use(middleware.Tenant(cached))
```

Inside handlers, read the resolved tenant with the typed accessors. `Find*`
returns the zero value when the tenant is absent, while `Get*` returns an
`errorx` error:

```go
t := middleware.FindTenant[*MyTenant](c) // *MyTenant, or nil if absent
t, err := middleware.GetTenant[*MyTenant](c) // err on absence or type mismatch
base := middleware.FindTenantBase(c)     // *tenant.Base, or nil
id, err := middleware.GetTenantID(c)     // uuid.UUID, err if absent
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
# Grace period after readiness flips to not-ready before shutdown proceeds.
# 0 (default) keeps the previous behavior; on Kubernetes set it to give
# kube-proxy time to drop the pod (see Graceful Shutdown).
shutdownDelay = "0s"

[http]
host = "0.0.0.0"
port = 8080
# Server timeouts (defaults shown). A value of "0s" disables that timeout;
# readHeaderTimeout still bounds the header read to cap slowloris exposure.
readTimeout = "10s"
readHeaderTimeout = "5s"
writeTimeout = "10s"
idleTimeout = "60s"
maxHeaderBytes = 1048576 # 1 MiB
# TLS: when both are set the server serves HTTPS.
# certFile = "/etc/tls/tls.crt"
# keyFile = "/etc/tls/tls.key"

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

Override via env vars: `APP_DATABASE_HOST=prodhost`, `APP_HTTP_PORT=443` (prefix defaults to `APP`, configurable via `host.WithEnvPrefix`).

### Extra Config

Implement `configx.Configurable` on your config struct and pass it to `app.Configure`:

```go
import "github.com/hatami57/microjet/core/configx"

type MyExtra struct {
 WorkerCount int    `mapstructure:"workerCount"`
 QueueName   string `mapstructure:"queueName"`
}

func (c *MyExtra) ReadConfig(l configx.Reader) error {
 return l.Read("myapp", c)
}

// At startup:
var extra MyExtra
app.Configure(&extra)
```

### Programmatic Config

File discovery is the default, but an embedded app (one started with
`Start`/`Wait`/`Shutdown` inside a larger process) or a test can supply
configuration in code instead.

Set individual values by their dotted path with `host.WithConfigValue` /
`host.WithConfigValues`. They win over config files, environment variables, and
defaults, and work with the default file reader too:

```go ignore
app := host.MustNew(
    host.WithConfigValue("app.name", "billing"),
    host.WithConfigValues(map[string]any{
        "app.shutdownDelay": "5s",
        "http.port":         8080,
    }),
)
```

To bypass file discovery entirely — an embedding host that already owns
configuration, or a hermetic test — inject a reader with `host.WithConfigReader`.
`configx.NewMapReader` provides an in-memory one seeded from a nested map that
mirrors the TOML layout (string values decode to typed fields, so `"5s"`
populates a `time.Duration`):

```go
import (
    "github.com/hatami57/microjet/core/configx"
    "github.com/hatami57/microjet/host"
)

func newApp() (*host.App, error) {
    reader := configx.NewMapReader(map[string]any{
        "app": map[string]any{"name": "billing", "shutdownDelay": "5s"},
        "log": map[string]any{"level": "debug"},
    })
    return host.New(host.WithConfigReader(reader))
}
```

Whatever an injected reader returns is authoritative — the env-var override shim
applies only to the built-in file reader.

## Error Handling

```go
import "github.com/hatami57/microjet/core/errorx"

// Builder-pattern enrichment
err := errorx.NewNotFoundError("User", "user not found").
    WithCode(1001).
    WithInner(fmt.Errorf("db: %w", originalErr))

// Sentinel errors
if err != nil {
    return errorx.ErrBadRequest.WithSubject("email")
}

// Check error type
switch {
case errorx.IsNotFoundError(err):
    // handle 404
case errorx.IsBadRequestError(err):
    // handle 400
}
```

## HTTP Hardening Middleware

Opt-in middleware (like CORS and rate limiting) for tightening the HTTP surface —
register them on the router or a route group:

```go
import "github.com/hatami57/microjet/httpx/middleware"

r := httpx.Of(app).Router
r.Use(middleware.SecureHeaders(middleware.DefaultSecureHeadersConfig())) // nosniff, X-Frame-Options, Referrer-Policy, HSTS (TLS only)
r.Use(middleware.BodyLimit(2 << 20))                                     // 413 when the body exceeds 2 MiB
r.Use(middleware.Timeout(15 * time.Second))                             // 503 when a request outlives its deadline
```

- `SecureHeaders(cfg)` — sets `X-Content-Type-Options: nosniff`, `X-Frame-Options`,
  `Referrer-Policy`, optional CSP, and HSTS (emitted only on TLS requests).
- `BodyLimit(maxBytes)` — rejects an oversized declared `Content-Length` with a
  clean 413 up front and caps chunked/undeclared bodies via `http.MaxBytesReader`.
- `Timeout(d)` — buffers the response and, if the handler outlives `d`, flushes a
  503 immediately and cancels the request context. Buffering means it is not for
  streaming (SSE) responses; the handler is not force-killed, only its context is
  cancelled.

For gzip, use [`gin-contrib/gzip`](https://github.com/gin-contrib/gzip) directly —
wrapping it would add nothing.

## HTTP Helpers

```go
import "github.com/hatami57/microjet/httpx"

// Typed param/query extraction (each returns an errorx error on a bad value)
id, _ := httpx.GetUUIDParam(c, "id")
name, _ := httpx.GetQuery(c, "name") // *string; nil when absent
pageSize, _ := httpx.GetInt64Query(c, "pageSize")

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

// Cursor-based pagination by ID. Filter by chaining Where on the table — the same
// Where/WhereIf/Order used by First, Count, and ListAll.
req := gormx.NewPageRequest[User, uint](httpx.PagedRequest(c), "id", func(u User) uint { return u.ID })

result, _ := userTable.Where("name ILIKE ?", "%john%").List(ctx, req)
for _, user := range result.Items {
    // ...
}
// result.NextPageToken is base64-encoded cursor for the next page

// Offset (page-number) pagination: set Page on the request, or call ForceOffset()
// to default a missing "page" query param to page 1 instead of cursor mode — for
// SQL-backed endpoints that always want page jumps with a computed TotalCount.
req = gormx.NewPageRequest[User, uint](httpx.PagedRequest(c).ForceOffset(), "id", func(u User) uint { return u.ID })
```

## Aggregates & projections

```go
// Aggregates scan a scalar into a dest pointer and compose with the chainable scopes.
// Sum/Avg/Max/Min cover numeric columns (empty set → 0 via COALESCE); Aggregate takes
// any raw SQL expression for the advanced cases.
var total uint64
orders.Where("is_confirmed = ?", true).Sum(ctx, "amount", &total)

var spread int
orders.Aggregate(ctx, "MAX(amount) - MIN(amount)", &spread, "is_confirmed = ?", true)

// Project maps rows into a result type instead of the entity — pair it with Select to
// pick or compute columns. dest is a *[]Result (or *[]map[string]any for ad-hoc shapes).
type tally struct {
    CampaignID uint
    Total      uint64
}
var rows []tally
orders.Select("campaign_id, COALESCE(SUM(amount), 0) AS total").
    Where("is_confirmed = ?", true).
    Group("campaign_id").
    Project(ctx, &rows)

// ProjectFirst is the single-row form; it reports whether a row was found.
var one tally
found, _ := orders.Select("campaign_id, amount AS total").Order("amount DESC").ProjectFirst(ctx, &one)
```

## Atomic & guarded updates

```go
// UpdateMap returns rows affected. Combine a gormx.Expr value (computed in-place by the
// database, no read-then-write) with a guard in the WHERE clause for a lock-free atomic
// compare-and-swap — a zero return means the guard rejected the update, not a failure.
// Portable across engines, SQLite included.
n, _ := campaigns.UpdateMap(ctx,
    map[string]any{"used": gormx.Expr("used + 1")},
    "id = ? AND used < capacity", id)
if n == 0 {
    // missing or at capacity
}

// For read-modify-write that can't be expressed as one statement, take a row lock inside
// a transaction. LockForUpdate emits SELECT … FOR UPDATE on Postgres/MySQL; on SQLite the
// clause is dropped (write safety comes from transaction-level serialization there).
repo.RunTx(ctx, func(ctx context.Context) error {
    acct, err := accounts.LockForUpdate().Get(ctx, "id = ?", id)
    if err != nil {
        return err
    }
    acct.Balance = recompute(acct.Balance)
    _, err = accounts.Update(ctx, acct)
    return err
})

// UpdateColumn(s) write raw columns without hooks or timestamp auto-updates; Raw/Exec are
// the escape hatch for SQL the builder can't express (both honor the ctx transaction).
var report []SalesRow
campaigns.Raw(ctx, &report, "SELECT region, SUM(total) AS total FROM orders GROUP BY region")
```

## Money

```go
import "github.com/hatami57/microjet/core/types/money"

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

### Multiple implementors of one interface

Register several implementors under one interface type by giving each a distinct
name, then resolve one by a discrete key — or enumerate them all and choose by a
runtime criterion:

```go
host.ProvideService[Notifier](app, emailNotifier, "email")
host.ProvideService[Notifier](app, smsNotifier, "sms")

// Pick by name (discrete key):
n := host.MustResolveService[Notifier](app, "sms")

// Enumerate every registered Notifier, keyed by name:
for name, n := range host.ResolveAllServices[Notifier](app) { /* ... */ }

// Or select the first one satisfying a predicate:
n, ok := host.ResolveServiceBy(app, func(n Notifier) bool {
    return n.Supports(channel)
})
```

Resolution is exact-type: an implementor is discoverable through an interface only
if it was registered under that interface type.

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
        WithModule(httpx.Module()).
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

## Metrics

`httpx.Module` serves Prometheus metrics at `GET /metrics` on a private registry
seeded with HTTP RED metrics (`http_requests_total`, `http_request_duration_seconds`)
plus the standard Go runtime and process collectors. To add your own collectors to
that same endpoint, register them from a `Setup` hook via `MetricsRegistry()`:

```go
package main

import (
    "github.com/hatami57/microjet/host"
    "github.com/hatami57/microjet/httpx"
    "github.com/prometheus/client_golang/prometheus"
)

func main() {
    widgets := prometheus.NewCounter(prometheus.CounterOpts{
        Name: "widgets_processed_total",
        Help: "Number of widgets processed.",
    })

    host.MustNew().
        WithModule(httpx.Module()).
        Setup(func(app *host.App) error {
            // Served at /metrics alongside the built-in HTTP, Go runtime,
            // and process metrics.
            httpx.Of(app).MetricsRegistry().MustRegister(widgets)
            return nil
        }).
        MustRun()
}
```

The registry is private by default (no global `prometheus.DefaultRegisterer`), so
`MetricsRegistry()` is the only way to add collectors to the endpoint.

## Graceful Shutdown

On a shutdown signal (or a fatal service error), `Run()` first flips every service
implementing `core.ReadinessToggler` — the HTTP server does — to not-ready, so
`/readyz` returns 503 `{"status":"shutting-down"}` while `/health` (liveness) stays
200. It then waits `app.shutdownDelay` before tearing anything down. On Kubernetes
set `shutdownDelay` to a few seconds (e.g. `"5s"`, with a matching
`terminationGracePeriodSeconds`) so kube-proxy removes the pod from its endpoints —
new requests stop arriving — before in-flight ones are drained. The default of
`"0s"` flips readiness and proceeds immediately.

`app.Close()` then drains messaging, stops the HTTP server, closes DB connections, and calls all registered `ServiceCloser` implementations — all running concurrently with a `sync.WaitGroup`. It is idempotent (guarded by `sync.Once`), so it is safe to call it via both `Run()`/`MustRun()` and a `defer app.Close()`.

## Examples

Runnable examples live in [`examples/`](examples/) — see the
[examples index](examples/README.md) for the full list. There is a focused,
single-feature example for each capability (errors, config, money, time,
converters, logging, cache, HTTP client, idempotency, tracing, migrations,
messaging, AWS) plus compound services that combine several:

- [`examples/minimal`](examples/minimal) — smallest possible app.
- [`examples/http-postgres`](examples/http-postgres) — HTTP CRUD backed by PostgreSQL.
- [`examples/sqlite`](examples/sqlite) — HTTP CRUD backed by SQLite; runs with no external database.
- [`examples/features`](examples/features) — middleware (CORS, rate limit, JWT), request-scoped logging, the cache, and the JSON client with retries.
- [`examples/modules`](examples/modules) — composable modules: a three-level dependency tree with shared-module deduplication.
- [`examples/outbox`](examples/outbox) — transactional outbox: enqueue an event in the same DB tx as the write, relayed to NATS.
- [`examples/compound-orders`](examples/compound-orders) — gormx + cache + idempotency + money + structured errors + pagination in one service.

The single-feature library examples (errors, money, time, converters, logging,
cache, HTTP client) are plain programs that print what they do and exit, so they
run offline with no setup.

For database migrations in production, see [`docs/migrations.md`](docs/migrations.md).

## Architecture

Every module builds on `core`; `host` depends only on `core`; satellite
modules depend on `core` + `host` and never on each other — except `outbox`
(which composes gormx + messaging) and `testx` (a harness over several).

```
core ─ errorx, configx, logx, jsonx, time, tenant, types (money), utils
  │
  └── host ─ orchestrator, DI container, module tree, lifecycle
        │
        ├── httpx      Gin server, middleware, JSON client
        ├── gormx      Table[T], drivers (postgres, sqlite), migrate
        ├── messaging  pub/sub abstraction  ── messaging/nats (driver)
        ├── cache      memory / Redis
        ├── otelx      OpenTelemetry tracing
        ├── aws        S3, SQS, DynamoDB
        ├── outbox     transactional outbox   (+ gormx, messaging)
        └── testx      test harness           (+ httpx, gormx/sqlite, messaging, cache)
```

## License

MIT
