# MicroJet Examples

Each subdirectory is a self-contained, runnable Go module. Most have **one
feature each** so you can see exactly how a single capability is used; a few are
**compound** examples that combine several features into a small realistic
service.

Run any example from its own directory:

```sh
cd examples/<name>
go run .
```

The library examples (errors, money, time, converters, logging, cache,
http-client) are plain programs that print what they do and exit — no server, no
external services, fully offline. Service examples that need infrastructure say
so in the table below.

## Single-feature examples

| Example | Feature | Needs | Notes |
|---|---|---|---|
| [`errors`](errors) | Structured errors (`core/errorx`) | — | 6 categories, builder enrichment, JSON shape, `errors.Is` by category |
| [`config`](config) | Configuration (`configx`) | — | TOML + defaults + typed sections; `APP_*` env overrides; `config.local.toml` overlay |
| [`config-validation`](config-validation) | Config validation hook (`configx.Validator`) | — | `Validate() error` rejects bad config at startup with a wrapped error |
| [`money`](money) | Money type (`core/types/money`) | — | currency-safe arithmetic, minor-unit conversion |
| [`time`](time) | Time utilities (`core`) | — | `TimeProvider`, `FixedClock`, sortable timestamps |
| [`converters`](converters) | Type converters (`jsonx`, `utils`) | — | JSON, struct↔map, pointer coalescing |
| [`logging`](logging) | Structured logging (`core/logx`) | — | levels, text/json, bound attributes |
| [`cache`](cache) | Cache (`cache`) | — | typed get/set, TTL expiry (in-memory; Redis via config) |
| [`http-client`](http-client) | HTTP client (`httpx.Client`) | — | retries + circuit breaker against an in-process test server |
| [`cors`](cors) | CORS middleware | — | allow-all vs a specific-origin allowlist (with credentials) and preflight |
| [`idempotency`](idempotency) | Idempotency middleware | — | `Idempotency-Key` replay backed by the cache |
| [`tracing`](tracing) | Distributed tracing (`otelx`) | OTLP collector (optional) | spans + `trace_id` log correlation |
| [`migrations`](migrations) | Versioned migrations (`gormx/migrate`) | — | embedded SQL Up/Down on SQLite |
| [`messaging`](messaging) | NATS pub/sub (`messaging`) | NATS | typed `HandleJSON` subscribe + periodic publish |
| [`jetstream`](jetstream) | Durable messaging (`messaging/jetstream`) | NATS (JetStream) | at-least-once delivery, acks, redelivery on failure, dead-letter subject |
| [`grpcx`](grpcx) | gRPC server + client (`grpcx`) | — | interceptors, errorx→status mapping, health, request-id trailer, `Dial` (in-process, offline) |
| [`aws`](aws) | AWS integration (`aws`) | AWS / LocalStack (optional) | S3, SQS, DynamoDB client init |

## Compound examples

| Example | Combines | Needs |
|---|---|---|
| [`minimal`](minimal) | host bootstrap + graceful shutdown | — |
| [`modules`](modules) | the module system / DI tree (diamond dedup) | — |
| [`features`](features) | HTTP server, CORS, rate limit, JWT, cache, request-scoped logging, JSON client | — |
| [`sqlite`](sqlite) | gormx (SQLite) CRUD + HTTP | — |
| [`http-postgres`](http-postgres) | gormx (Postgres) CRUD + cursor pagination | PostgreSQL |
| [`outbox`](outbox) | gormx + messaging + transactional outbox relay | NATS |
| [`compound-orders`](compound-orders) | gormx + cache + idempotency + money + errors + pagination | — |

## Running the infrastructure-backed examples

```sh
# NATS (messaging, outbox)
docker run --rm -p 4222:4222 nats:latest

# NATS with JetStream enabled (jetstream) — durable streams need the -js flag
docker run --rm -p 4222:4222 nats:latest -js

# PostgreSQL (http-postgres) — see that example's config.toml for credentials
docker run --rm -p 5432:5432 -e POSTGRES_PASSWORD=postgres postgres:16

# OTLP collector for tracing (optional) — view spans in Jaeger at :16686
docker run --rm -p 16686:16686 -p 4318:4318 jaegertracing/all-in-one
```

Each example is its own module with `replace` directives pointing at the
in-repo packages, and is listed in the top-level `go.work`, so `go build`/`go
test` over the workspace covers them like any other module.
