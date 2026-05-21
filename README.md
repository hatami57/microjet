# microjet

A Go micro-framework for building production-grade microservices with minimal boilerplate.

```go
import "github.com/hatami57/microjet/host"
```

## Features

- **Application Orchestrator** — Fluent builder API (`New().WithHTTP().WithPostgreSQL().WithMessaging().MustRun(...)`) with dependency injection container and graceful shutdown.
- **Structured Errors** — Typed error system with 6 categories (BadRequest, NotFound, Business, Unauthorized, Forbidden, Internal), builder-pattern enrichment, sentinel errors, and `errors.As` extraction.
- **Configuration** — TOML-based config loading with environment variable overrides, local config merging, post-load hooks, and generic typed access to arbitrary sections.
- **HTTP Server** — Gin-based server with built-in middleware (structured logging, error translation, recovery), health endpoint, Swagger UI, typed param/query/body binding, multi-tenant support, and graceful shutdown.
- **PostgreSQL / GORM** — Generic `Table[T]` with CRUD, cursor-based pagination (by ID or created_at), transactions, batch inserts, and eager loading.
- **AWS Integration** — Unified S3 (single/concurrent download, upload), SQS (send JSON messages), and DynamoDB client initialization.
- **NATS Messaging** — Pub/sub with typed message envelopes and graceful drain.
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
	app := host.New()
	defer app.Close()

	app.Logger.Info("started")
	host.WaitForExitSignal()
}
```

### With Custom App Config

```go
type MyConfig struct {
	ServiceName string `mapstructure:"serviceName"`
	MaxWorkers  int    `mapstructure:"maxWorkers"`
}

func main() {
	app := host.NewWithEnvPrefix("MYAPP")
	defer app.Close()
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
	app := host.New()
	defer app.Close()

	app.WithPostgreSQL().
		WithHTTPServer(func(a *host.App) error {
			userTable := postgres.NewTable[User](a.DB)
			a.HTTPServer.Router.GET("/users", func(c *gin.Context) {
				items, _ := userTable.ListAll(c.Request.Context(),
					postgres.NewListRequestByID[User](httpx.PagedRequest(c)))
				c.JSON(200, items)
			})
			return nil
		}).
		MustRun(func(a *host.App) error {
			return a.DB.AutoMigrate(&User{})
		})

	go app.StartHTTP()
	host.WaitForExitSignal()
}
```

## Configuration

Create `config.toml` (auto-discovered from working directory, `./config/`, or exe path):

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

Override via env vars: `APP_DATABASE_HOST=prodhost`, `APP_SERVER_PORT=443` (prefix defaults to `APP`, configurable via `NewWithEnvPrefix`).

### Extra Config

```go
var cfg CustomExtra
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
    return core.ErrInvalidInput.WithSubject("email")
}

// Check error type
switch {
case core.IsNotFoundError(err):
    // handle 404
case core.IsInvalidInputError(err):
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

app := host.New()
host.ProvideService(app, &UserService{})

// Later:
svc, ok := host.ResolveService[*UserService](app)
```

## Graceful Shutdown

`app.Close()` drains messaging, stops HTTP server, closes DB connections, and calls all registered `ServiceCloser` implementations — all running concurrently with a `sync.WaitGroup`.

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
