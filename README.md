# microjet

[![CI](https://github.com/hatami57/microjet/actions/workflows/ci.yml/badge.svg)](https://github.com/hatami57/microjet/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/hatami57/microjet.svg)](https://pkg.go.dev/github.com/hatami57/microjet)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A Go micro-framework for building production-grade microservices with minimal boilerplate.

```go
import "github.com/hatami57/microjet/host"
```

## Features

- **Application Orchestrator** — Fluent builder API (`MustNew().WithPostgreSQL().WithHTTPServer(...).MustRun()`) with deferred error handling, a dependency injection container, and managed graceful shutdown.
- **Structured Errors** — Typed error system with 6 categories (BadRequest, NotFound, Business, Unauthorized, Forbidden, Internal), builder-pattern enrichment, sentinel errors, and `errors.As` extraction.
- **Configuration** — TOML-based config loading with environment variable overrides, local config merging, post-load hooks, and generic typed access to arbitrary sections.
- **HTTP Server** — Gin-based server with built-in middleware (structured logging, error translation, recovery), health endpoint, Swagger UI (debug mode only), typed param/query/body binding, multi-tenant support, and graceful shutdown.
- **PostgreSQL / GORM** — Generic `Table[T]` with CRUD, cursor-based pagination (by ID or created_at), transactions, batch inserts, and eager loading.
- **AWS Integration** — Unified S3 (single/concurrent download, upload), SQS (send JSON messages), and DynamoDB client initialization.
- **NATS Messaging** — Pub/sub with raw-byte delivery; pair with `types.Message` for structured JSON envelopes and graceful drain.
- **Money Type** — Currency-aware decimal arithmetic (`Add`, `Sub`, `Multiply`) with currency validation.
- **Time Utilities** — `TimeProvider` interface for testability, sortable timestamp formats.
- **Type Converters** — Generic JSON, struct-to-map, and pointer coalescing utilities.
- **Logging** — Structured `log/slog` with configurable level and format (text/JSON).

## Packages

| Package | Import | Description |
|---|---|---|
| `host` | `github.com/hatami57/microjet/host` | Application orchestrator, DI container, lifecycle |
| `core` | `github.com/hatami57/microjet/core` | Errors, config loading, logging, time |
| `http` | `github.com/hatami57/microjet/http` | Gin HTTP server, middleware, request helpers |
| `postgres` | `github.com/hatami57/microjet/postgres` | Generic GORM CRUD + cursor pagination |
| `messaging` | `github.com/hatami57/microjet/messaging` | NATS pub/sub client |
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

func main() {
	app := host.MustNew(host.WithEnvPrefix("MYAPP"))
	defer app.Close()

	cfg, err := core.GetExtra[MyConfig](app.Config, "myapp")
	if err != nil {
		panic(err)
	}
	app.Logger.Info("started", "service", cfg.ServiceName)
	host.WaitForExitSignal()
}
```

### Full HTTP + PostgreSQL

```go
package main

import (
	"github.com/gin-gonic/gin"
	httpx "github.com/hatami57/microjet/http"
	"github.com/hatami57/microjet/host"
	"github.com/hatami57/microjet/postgres"
)

type User struct {
	ID    uint   `gorm:"primaryKey"`
	Name  string `gorm:"not null"`
	Email string `gorm:"unique;not null"`
}

func main() {
	app := host.MustNew()

	app.WithPostgreSQL().
		Setup(func(a *host.App) error {
			return a.DB.AutoMigrate(&User{})
		}).
		WithHTTPServer(func(a *host.App) error {
			userTable := postgres.NewTable[User](a.DB)
			a.HTTPServer.Router.GET("/users", func(c *gin.Context) {
				items, _ := userTable.ListAll(c.Request.Context(),
					postgres.NewListRequestByID[User](httpx.PagedRequest(c)))
				c.JSON(200, items)
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

```go
cfg, err := core.GetExtra[CustomExtra](app.Config, "mySection")
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
import httpx "github.com/hatami57/microjet/http"

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
    httpx "github.com/hatami57/microjet/http"
    "github.com/hatami57/microjet/postgres"
)

// Cursor-based pagination by ID
req := postgres.NewListRequestByID[User](httpx.PagedRequest(c)).
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

## Graceful Shutdown

`app.Close()` drains messaging, stops the HTTP server, closes DB connections, and calls all registered `ServiceCloser` implementations — all running concurrently with a `sync.WaitGroup`. It is idempotent (guarded by `sync.Once`), so it is safe to call it via both `Run()`/`MustRun()` and a `defer app.Close()`.

## Examples

Runnable example services live in [`examples/`](examples/):

- [`examples/minimal`](examples/minimal) — smallest possible app.
- [`examples/http-postgres`](examples/http-postgres) — HTTP CRUD backed by PostgreSQL.

## Architecture

```
utils  (JSON, converters, env)
  |
  +-- types  (Message, PagedResult, money)
  |     +-- postgres  (Table[T], pagination)
  |
  +-- aws  (S3, SQS, DynamoDB)
  |
core  (errors, config, logging, time)
  |
  +-- messaging  (NATS client)
  +-- http  (Gin server, middleware, helpers)
        |
        +-- host  (orchestrator, DI, lifecycle)
              imports: aws, core, http, messaging, postgres, types, utils
```

## License

MIT
