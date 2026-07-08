# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims to
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.27.0] - 2026-07-08

### Added

- **Optional key-value params on `errorx` constructors** — `NewError` and every category
  constructor (`NewBadRequestError`, `NewNotFoundError`, `NewBusinessError`,
  `NewUnauthorizedError`, `NewForbiddenError`, `NewInternalError`) now take trailing
  `paramKeyVals ...any` that are merged into `Params` with the same semantics as
  `WithParams`: string keys, a non-string key surfaced under `!BADKEY`, and a dangling
  trailing key dropped. The variadic signature is backward compatible with existing calls.
- **`gormx.Table[T].Omit(columns...)`** — wraps GORM's `Omit` as the complement of `Select`:
  on reads it excludes the columns from the `SELECT`; on `Create`/`Save`/`Update` it excludes
  them from the written columns. Chainable and immutable like the other builder scopes.

## [0.26.0] - 2026-07-06

### Added

- **`PagedResultRequest.ForceOffset()` (`core/types`)** — opts a listing request into offset
  (page-number) pagination when the caller didn't pick a mode, defaulting a missing `page`
  query param to page 1 instead of cursor mode. Intended for SQL-backed endpoints that always
  want page jumps with a computed `TotalCount`.

## [0.25.0] - 2026-07-06

### Added

- **Configurable HTTP server timeouts (`httpx`)** — `readTimeout` (10s), `readHeaderTimeout`
  (5s, caps slowloris even when `readTimeout=0`), `writeTimeout` (10s), and `idleTimeout`
  (60s) are now set under `[http]`, with the previous hardcoded values as defaults. Adds
  `maxHeaderBytes` (default 1 MiB) and TLS via `certFile`/`keyFile` (the server uses
  `ListenAndServeTLS` when both are set).
- **New opt-in `httpx` middleware** — `BodyLimit` (413 for oversized declared
  `Content-Length`, with a `MaxBytesReader` backstop for chunked/undeclared bodies),
  `Timeout` (buffers the response and flushes a 503 the instant the deadline fires while
  cancelling the request context), and `SecureHeaders` (`nosniff`, `X-Frame-Options`,
  `Referrer-Policy`, optional CSP, and HSTS emitted only on TLS requests).
- **pprof endpoints in debug mode (`httpx`)** — `net/http/pprof` is mounted under
  `/debug/pprof` behind the same `[http]` debug gate as Swagger, so profiling is available
  in development but never reachable on a production port.
- **Kubernetes-aware graceful shutdown (`core` + `host` + `httpx`)** — new
  `core.ReadinessToggler` interface; `httpx.Server` implements it so that during shutdown
  `/readyz` returns `503 {"status":"shutting-down"}` (without running checks) while
  `/health` liveness stays `200`. `Run()` flips every `ReadinessToggler` to not-ready and
  waits `[app] shutdownDelay` (default 0s) before cancelling workers and closing services,
  letting load balancers drop the pod before in-flight requests drain.

### Changed

- **Build** — the `Makefile` now auto-discovers modules with the same `find`-based
  discovery CI uses (excluding `examples/`), so a newly added module can no longer silently
  escape `build`/`vet`/`test`/`lint`.
- **Docs** — fixed stale API references across the README and `docs/migrations.md` to match
  the current multi-module surface (`core/errorx`, `configx.Reader`, `gormx.Module`/
  `gormx.Of`, `tenant.NewCachedStore`), redrew the architecture diagram, and renamed the
  root `issues.md` roadmap to `ROADMAP.md`.

## [0.24.0] - 2026-06-29

### Added

- **`gormx.Table[T]` raw-SQL escape hatches** — `Raw(ctx, dest, sql, values...)` runs a
  query and scans into a struct, slice, map, or scalar; `Exec(ctx, sql, values...)`
  runs a no-result statement (INSERT/UPDATE/DELETE/DDL) and returns rows affected. Both
  run on the transaction in `ctx` when present (see `RunTx`); the SQL is engine-specific
  and the Table's builder scopes (`Where`/`Order`/`Preload`) do not apply.
- **`gormx.Table[T].UpdateColumn` / `UpdateColumns`** — raw-column writes that report rows
  affected but, unlike `UpdateMap`, skip model hooks and timestamp auto-updates (e.g.
  `UpdatedAt`). Use them for bulk maintenance writes where those side effects are unwanted.
- **`gormx.Table[T].Pluck`** — collects a single column into a slice keeping duplicates,
  the non-deduplicating counterpart to `PluckDistinct`.
- **`gormx.Table[T].FindInBatches`** — streams matching rows to a callback in fixed-size
  batches so large result sets are processed without loading every row at once; honors
  chained `Where`/`Order`/`Preload` and stops on the first error the callback returns.
- **`gormx.Table[T].LockForUpdate`** — chainable scope emitting `SELECT … FOR UPDATE` for
  pessimistic read-modify-write inside `RunTx`. A Postgres/MySQL feature: the SQLite driver
  silently drops the locking clause (write safety there comes from transaction-level
  serialization), and outside a transaction it is a no-op on every engine.
- **`gormx` GORM re-exports** — `Expr` (alias for `gorm.Expr`), `DB` (`gorm.DB`),
  `OrderByColumn` (`clause.OrderByColumn`), and `Associations` (`clause.Associations`), so
  callers can use the symbols that appear in `gormx`'s own API without importing
  `gorm.io/gorm` directly. Notably, pass `gormx.Expr("count + 1")` as an `UpdateMap` value
  for lock-free atomic, in-place updates.

### Changed

- **`gormx.Table[T].UpdateMap` now returns `(int64, error)`** (breaking) — it reports how
  many rows were updated in addition to the error. The count matters for capacity- or
  version-guarded updates whose `WHERE` clause may match no rows (e.g. an atomic counter
  increment bounded by a limit): a zero return means the guard rejected the update rather
  than a failure. Callers that don't need the count can discard it with `_`.

## [0.23.0] - 2026-06-27

### Added

- **`Lookup` accessors** — every module that exposes `Of` now also exposes a
  `Lookup(app, name...) (T, bool)` companion that reports whether the service is
  registered instead of panicking: `aws`, `cache`, `gormx`, `httpx`, `messaging`,
  and `otelx`. Use `Lookup` where absence is an expected, recoverable condition
  (e.g. a background worker); prefer `Of` when the service must exist.

### Changed

- **`Of` accessors now panic when the service is not registered** (breaking) —
  `cache.Of`, `gormx.Of`, and `messaging.Of` previously returned `nil` (or a nil
  `*gorm.DB`) when no service was installed, deferring the failure to a confusing
  nil-pointer dereference far from the cause. They now fail fast via
  `host.MustResolveService`, matching `aws.Of`, `httpx.Of`, and `otelx.Of`. A
  missing registration is a wiring bug; callers that genuinely want the optional
  behavior should use the new `Lookup` accessors.

## [0.22.0] - 2026-06-27

### Added

- **`tenant.CachedStore.Clear`** — drops every cached entry in one call, forcing
  subsequent `FindTenant` lookups to consult the wrapped store. Complements the
  existing per-entry `Invalidate(id)`. Safe for concurrent use.
- **`middleware.GetTenant[T]`** — error-returning counterpart to `FindTenant[T]`.
  Returns `errorx.ErrNotFound` when no tenant is in the context and
  `errorx.ErrInternal` when the stored tenant is not a `T`, mirroring
  `GetTenantID`'s semantics.

### Changed

- **`middleware.GetTenant[T]` → `middleware.FindTenant[T]`** (breaking) — renamed
  the nil-returning accessor to the `Find*` convention, since it yields the zero
  value rather than an error when no tenant is present. Its type parameter is now
  constrained to `tenant.Tenant`, so it is instantiated with the pointer type and
  returns it directly: `FindTenant[*MyTenant](c)` returns `*MyTenant` (previously
  `FindTenant[MyTenant](c)` returned `*MyTenant`).
- **`middleware.GetTenantBase` → `middleware.FindTenantBase`** (breaking) — renamed
  for the same reason; it returns `*tenant.Base` or nil without an error.

## [0.21.0] - 2026-06-24

### Added

- **`gormx.Table[T]` aggregates** — `Aggregate`, `Sum`, `Avg`, `Max`, and `Min` scan a
  scalar aggregate into a caller-supplied dest pointer, e.g.
  `table.Where("is_confirmed = ?", true).Sum(ctx, "amount", &total)`. They mirror
  `Count`/`PluckDistinct`: accept optional GORM-style where conditions and compose with
  accumulated `Where`/`WhereIf` scopes. `Sum`/`Avg`/`Max`/`Min` cover numeric columns and
  return zero (via `COALESCE`) rather than NULL on an empty set, so dest is always written.
  `Aggregate` is the general primitive behind them — pass any raw SQL aggregate expression
  (e.g. `"MAX(price) - MIN(price)"`) for advanced cases without a dedicated helper.
- **`gormx.Table[T].Project` / `ProjectFirst`** — run a Table's query but map the rows into a
  caller-supplied dest instead of the entity, for read models / DTOs. They reuse all chained
  scopes (`Select`/`Where`/`Order`/`Group`/`Joins`/…), so pair them with `Select` to pick or
  compute columns, e.g.
  `table.Select("color, COALESCE(SUM(price),0) AS total").Group("color").Project(ctx, &rows)`.
  `Project` takes a `*[]Result` (or `*[]map[string]any` for ad-hoc shapes); `ProjectFirst`
  takes a `*Result` and returns `(found bool, err error)`, leaving dest untouched and
  reporting `false` when no row matches rather than treating it as an error.

## [0.20.0] - 2026-06-24

### Added

- **`gormx.Table[T]` chainable query builder** — new copy-on-write builder methods
  that accumulate as scopes and apply to every read on the table: `Order`, `Limit`,
  `Offset`, `Select`, `Distinct`, `Joins`, `Group`, `Having`, and `Unscoped`
  (include soft-deleted rows / hard delete). They compose with the existing
  `Where`/`WhereIf`/`Preload` and never mutate the original table, so a scoped
  handle (`scoped := table.Where("tenant_id = ?", id)`) auto-applies to all reads
  through it.
- **`gormx.Table[T]` single-row getters** — `First`, `Last`, and `Take` return the
  matching row or `nil` when none matches (no error); `Get` is like `First` but
  returns an `errorx.ErrNotFound` (a NotFound-category error, subject set to the
  entity type name, so the HTTP error middleware maps it to 404) when the row is
  absent. `Exists` reports whether any row matches.

### Breaking

- **`gormx.Table[T].Find` removed — use `First`** — `Find` returned a single row,
  which collided with GORM's own `Find` (it populates a slice). Single-row reads
  now use `First`/`Last`/`Take`/`Get`; multi-row reads use `ListAll`/`List`/`ListPage`.
  Migration: replace `table.Find(ctx, …)` with `table.First(ctx, …)`, or `table.Get(ctx, …)`
  when a missing row should surface as a not-found error.
- **`gormx.Table[T].ListAll` no longer takes an `orderBy` argument** — ordering now
  comes from the chainable `Order` scope. Migration: `table.ListAll(ctx, "created_at DESC")`
  → `table.Order("created_at DESC").ListAll(ctx)`.
- **`gormx` pagination filtering unified on chaining** — `PageRequest.SetWhere`
  (and its backing `Where()` method) and the `Where() []any` method on the
  `ListRequest` interface and `BaseListRequest` are removed. Filter paginated
  queries by chaining `Where`/`WhereIf` on the table, the same as every other read;
  the optional `Scoper` interface remains for request-driven filtering and is now
  applied on top of the table's scopes. Migration:
  `NewPageRequest(...).SetWhere("tenant_id = ?", id)` then `table.List(ctx, req)`
  → `table.Where("tenant_id = ?", id).List(ctx, req)`.

## [0.19.0] - 2026-06-23

### Breaking

- **Infrastructure wiring is now module-based** — every built-in capability is
  installed uniformly through `WithModule(...)` instead of a dedicated `With*`
  method, and the typed `App` fields/accessors are replaced by package-level
  `Of(app)` helpers. The motivation is dependency hygiene: the `host` module no
  longer imports `aws`, `cache`, `gormx`, `httpx`, `messaging`, `otelx`, or
  `outbox`, so importing `host` no longer pulls in the AWS SDK, GORM, Gin, Redis,
  and Mongo. You depend only on the satellite modules you actually install.

  Migration:
  - `app.WithHTTPServer(...)` → `app.WithModule(httpx.Module())` (register routes
    in a `Setup` hook); `app.HTTPServer` → `httpx.Of(app)`
  - `app.WithDatabase(d)` → `app.WithModule(gormx.Module(d))`;
    `app.WithNamedDatabase(n, d)` → `gormx.Module(d, n)`;
    `app.InjectDatabase(db)` → `gormx.Inject(db)`;
    `app.DB()` → `gormx.Of(app)`; `app.NamedDB(n)` → `gormx.Of(app, n)`
  - `app.WithMessaging(c)` → `app.WithModule(messaging.Module(c))`;
    `app.WithSubscriber(...)` → `messaging.Subscribe(...)`;
    `host.WithQueueGroup` → `messaging.WithQueueGroup`;
    `app.Messaging` → `messaging.Of(app)`
  - `app.WithCache()` → `app.WithModule(cache.Module())`;
    `app.WithCacheClient(c)` → `cache.ModuleWithClient(c)`;
    `app.Cache()` → `cache.Of(app)`
  - `app.WithAWS(svcs...)` → `app.WithModule(aws.Module(svcs...))`;
    `app.AWS` → `aws.Of(app)`
  - `app.WithTracing()` → `app.WithModule(otelx.Module())`
  - `app.WithOutbox(opts...)` → `app.WithModule(outbox.Module(opts...))`; the
    `host.WithOutbox*` options become `outbox.Interval`/`BatchSize`/`Database`
  - `app.StartHTTP()` is removed; use `Run`/`MustRun`, or drive the lifecycle
    manually with `app.InitServices()` + `host.WaitForExitSignal()`

- **HTTP config section renamed `[server]` → `[http]`** — the default config
  section the HTTP server reads is now `[http]` (named servers read
  `[http.<name>]`). Rename the `[server]` table in your config files and update
  env overrides accordingly: `APP_SERVER_PORT` → `APP_HTTP_PORT`,
  `APP_SERVER_HOST` → `APP_HTTP_HOST`. A leftover `[server]` section is silently
  ignored, leaving the server on its defaults (`localhost:8080`).

- **`Message` moved to `messaging` and renamed `Envelope`** — the message
  envelope type that lived in `core/types` is now `messaging.Envelope`, keeping
  the broker payload type in the package that owns messaging. Update references
  from `types.Message` to `messaging.Envelope`; message handlers now receive a
  `*messaging.Envelope`.

- **`host.WithShutdownTimeout` renamed to `host.WithCloseTimeout`** — the option
  name now matches the lifecycle vocabulary (`Closer`/`CloseOrderer`/`CloseTimeout`)
  used everywhere else. Replace `host.WithShutdownTimeout(d)` with
  `host.WithCloseTimeout(d)`.

- **`httpx/middleware` `IdempotencyStore` uses `GetBytes`/`SetBytes`** — the
  interface declared `Get`/`Set`, but `cache.Cache` exposes byte-oriented
  `GetBytes`/`SetBytes` (the `Get`/`Set` pair now operates on `any`). The store
  interface is realigned so `cache.Of(app)` satisfies `IdempotencyStore` directly,
  removing the adapter shim. Custom stores must rename their methods to
  `GetBytes`/`SetBytes`.

### Added

- **Named service instances** — the DI container is now keyed by `(type, name)`
  instead of type alone, so several instances of one type coexist. Every module
  constructor and `Of` accessor takes an optional name: `httpx.Module("admin")` /
  `httpx.Of(app, "admin")`, `cache.Module("sessions")`, `gormx.Module(d, "analytics")`,
  `messaging.Module(c, "events")`, `aws.NamedModule("eu", ...)`. Named `httpx` and
  `cache` instances read their own config section (`[http.<name>]`,
  `[cache.<name>]`) so a second HTTP server can bind a different port. `Of(app)`
  and the unnamed form are unchanged; the empty name is the default instance.
  `host.ProvideService`/`ResolveService`/`MustResolveService` gain a trailing
  `name ...string`.
- **Ordered graceful shutdown** — services now close in dependency-safe order
  instead of the previous unspecified `sync.Map` order. Inbound edges (HTTP
  servers, message subscribers) close first so they stop accepting work and drain
  in-flight requests, then ordinary services, then backends (database, cache,
  broker, tracing) — so a draining HTTP handler still has its database. Closing is
  sequential across bands and remains bounded by the shutdown timeout. A service
  can opt into a band by implementing the new optional `host.CloseOrderer`
  (`CloseOrder() int`); use the `host.CloseEdge`/`CloseDefault`/`CloseBackend`
  constants. Services that don't implement it close at `CloseDefault`.
- **Module de-duplication for infrastructure** — satellite `Module` constructors
  now deduplicate by `(slot, name)`, so installing e.g. `httpx.Module()` twice
  registers one server instead of silently creating a second that clobbers the
  first. Distinct names still coexist; `messaging.Subscribe` stays additive (each
  call adds a binding). Backed by the new `host.KeyedModuleFunc(key, fn)` helper.
- **Unused config-section warnings** — at the end of service initialization the
  host logs a warning for each config-file top-level section that nothing read,
  catching typos and renamed sections (e.g. a stale `[server]` after the rename to
  `[http]`) that otherwise silently have no effect. Exposed via the reader's
  `UnusedSections() []string`.
- **`host.App.RangeServices`** — iterate registered services without access to
  container internals, so satellite packages can aggregate health checks, etc.
- **`host.ErrSource`** — optional `ErrCh() <-chan error` interface; `Run` now
  reacts to *any* started service's fatal channel (generalizing the old
  HTTP-server-only handling).

### Removed

- Dead DI primitives that had no callers: `host.ProvideType`, `host.ResolveType`,
  `host.ProvidedItem`, `App.ProvideService(*ProvidedItem)`, `App.ProvideServices`,
  and the `reflect.Type`-keyed `App.ResolveService`/`App.MustResolveService`
  methods. Use the generic `host.ProvideService`/`ResolveService` functions.

### Fixed

- **`configx` now honors environment-variable overrides in struct unmarshal** —
  viper's `Unmarshal`/`UnmarshalKey` assemble a section from the file and default
  layers only and do not consult `AutomaticEnv`, so `APP_<SECTION>_<KEY>`
  overrides silently had no effect. `Read`/`ReadAll` now reflect over the
  destination struct, bind an env var for every leaf key, and promote each
  resolved value (env > file > default) into viper's override layer, which
  `Unmarshal` does read.

## [0.18.0] - 2026-06-17

### Added

- **Offset pagination for `gormx`** — `types.PagedResultRequest` gains an optional
  1-based `Page` field. When set, `gormx.Table.List` switches from forward-only
  cursor pagination to offset pagination: it ignores the cursor token, skips
  `(Page-1)*PageSize` rows, and computes `TotalCount` so callers can render
  "page X of Y" and jump to an arbitrary page. When `Page` is nil the existing
  cursor behaviour is unchanged. `httpx.PagedRequest` reads the `page` query
  param to opt in; a malformed `page` falls back to cursor mode.
  - New optional `gormx.OffsetPager` interface (`Offset() (offset int, ok bool)`),
    alongside `Scoper`/`CursorScoper`. `PageRequest` implements it.
  - Offset pagination is SQL-only. Cursor-only backends (DynamoDB) do not honor
    `Page`.

## [0.17.0] - 2026-06-15

### Breaking

- **`core/config` renamed to `core/configx`** — the package was named `config`,
  the same identifier most consumers use for their own application config
  package, forcing an import alias whenever both were used together. It now
  follows the project's `x`-suffix convention (`errorx`, `logx`, `jsonx`, …) so
  the import resolves to `configx` and no longer collides. Update imports:
  - `github.com/hatami57/microjet/core/config` →
    `github.com/hatami57/microjet/core/configx`
  - `config.Configure`, `config.Reader`, `config.Configurable`,
    `config.NewViperConfigReader` → `configx.*`

## [0.16.0] - 2026-06-15

### Changed

- **Cache value API split** — `cache.Cache` now exposes byte-oriented
  `GetBytes`/`SetBytes` (the previous `Get`/`Set` behaviour) alongside new
  `Get`/`Set` that operate on `any`. `cache.New` and `cache.NewMemoryCache` take
  a `core.TimeProvider` so expiry is driven by the injected clock.
- **Foundation modules merged into `core`** — the `versioninfo`, `utils`,
  `jsonx`, `types`, and `tenant` modules have been folded into the `core` module
  as subpackages. They carried only light, ubiquitous dependencies and were
  versioned in lockstep with `core`, so separate modules added release overhead
  without an opt-out benefit. Heavyweight, swappable modules (`aws`, `cache`,
  `httpx`, `otelx`, `messaging`/`messaging/nats`, the `gormx` driver tree,
  `outbox`) remain separate so consumers only pull the dependencies they import.

- **`core` split into focused subpackages** — the three largest concerns moved
  out of the top-level `core` package into their own subpackages so the umbrella
  package holds only cross-cutting primitives (time, correlation, lifecycle
  interfaces):
  - typed errors → `core/errorx`
  - logging/slog setup → `core/logx`
  - config loading → `core/config`

  `core.TimeProvider`/`core.Clock`, correlation helpers, and the lifecycle
  interfaces (`Closer`/`Starter`/…) stay in `core`.

### Breaking

- `cache.Cache.Get`/`Set` now take and return `any` rather than `[]byte`; the
  byte-oriented behaviour moved to `GetBytes`/`SetBytes`. `cache.New` and
  `cache.NewMemoryCache` gained a `core.TimeProvider` parameter.
- The selectors for the split-out `core` APIs changed (the package qualifier,
  not just the import path):
  - `core.NewError`, `core.Err*`, `core.Is*Error`, `core.Error`, `core.ErrorType`,
    `core.ErrorResponse`, `core.*ErrorType` → `errorx.*`
    (`github.com/hatami57/microjet/core/errorx`)
  - `core.NewLogger`, `core.LogConfig`, `core.LogOutputConfig` → `logx.*`
    (`github.com/hatami57/microjet/core/logx`)
  - `core.Configure`, `core.ConfigReader`, `core.Configurable`,
    `core.NewViperConfigReader` → `config.*`
    (`github.com/hatami57/microjet/core/config`)
- Import paths changed. Update imports as follows:
  - `github.com/hatami57/microjet/versioninfo` → `github.com/hatami57/microjet/core/version` (also renamed `versioninfo` → `version`)
  - `github.com/hatami57/microjet/utils` → `github.com/hatami57/microjet/core/utils`
  - `github.com/hatami57/microjet/jsonx` → `github.com/hatami57/microjet/core/jsonx`
  - `github.com/hatami57/microjet/types` (and `types/money`) → `github.com/hatami57/microjet/core/types`
  - `github.com/hatami57/microjet/tenant` → `github.com/hatami57/microjet/core/tenant`
- Build-time version stamping must target the new package path, e.g.
  `-X github.com/hatami57/microjet/core/version.Version=1.2.3`.
- Remove the `require`/`replace` entries for the five removed modules from
  consuming `go.mod` files; the packages now ship with
  `github.com/hatami57/microjet/core`.

## [0.15.0] - 2026-06-14

### Added

- **`testx` module** — new `github.com/hatami57/microjet/testx` module of test
  helpers: `NewApp` builds a `host.App` with deterministic in-memory defaults
  (FixedClock, in-memory SQLite, in-memory cache) and registers cleanup; `NewDB`
  returns a throwaway `*gorm.DB`; `Broker` is an in-memory `messaging.Client`
  fake for pub/sub and request-reply; and `Request`/`DecodeJSON`/`AssertStatus`
  exercise a gin router over httptest.
- **`outbox` module** — new `github.com/hatami57/microjet/outbox` module
  implementing the transactional outbox pattern. `outbox.Enqueue` /
  `EnqueueJSON` record a broker message in the same gorm transaction as a domain
  write; `outbox.Relay` (and `host.WithOutbox`) publish pending rows to a
  `messaging.Publisher` with at-least-once delivery, recording per-message
  attempts/errors on failure. `host.WithOutbox` migrates the table and runs the
  relay as a periodic worker.
- **`gormx/migrate` module** — new opt-in
  `github.com/hatami57/microjet/gormx/migrate` module wrapping goose for
  versioned SQL migrations. Derives the goose dialect from the gorm driver
  (Postgres/SQLite/MySQL), reads embedded migration files, and exposes
  `Up`/`Down`/`Version` plus a one-call `migrate.Up` for use from a host Setup
  handler. The core framework gains no migration-tool dependency.
- **`otelx` module** — new `github.com/hatami57/microjet/otelx` module providing
  OpenTelemetry tracing setup: OTLP/HTTP exporter, W3C trace-context + baggage
  propagation, ratio-based sampling, and lifecycle management (flush on
  shutdown). Configured from the `[tracing]` section; service name/version
  default to the `[app]` section.
- **`host.App.WithTracing`** — registers the otelx service so the whole stack
  traces automatically.
- **HTTP server tracing** — new `middleware.Tracing` (installed by default)
  starts a server span per request, continues incoming `traceparent` headers,
  and names spans by route pattern. The request logger now also carries
  `trace_id` alongside `request_id`.
- **HTTP client tracing** — `httpx.Client` starts a client span per attempt and
  injects the W3C trace context into outbound requests.
- **GORM tracing** — `gormx.UseTracing(db)` registers span callbacks for all
  operations (applied automatically to driver-opened connections); spans record
  the table, SQL template (placeholders only), and errors.
- **NATS tracing & propagation** — `messaging.InjectContext` /
  `messaging.ExtractContext` carry the trace context and correlation id through
  broker headers; the NATS client wraps publish/request/receive/respond in
  producer/client/consumer/server spans and hands handlers a context carrying
  the remote trace and correlation id.
- **`host.App.WithSubscriber`** — registers a message subscription tied to the
  app lifecycle: it subscribes once the broker is connected and the app starts,
  and unsubscribes on shutdown. `host.WithQueueGroup` joins a queue group to
  load-balance a subject across replicas. Multiple subscribers share one
  lifecycle-managed consumer.
- **Typed message handlers** — `messaging.HandleJSON[T]` and
  `messaging.HandleEnvelope[T]` adapt typed handlers (`func(ctx, T) error`) into
  raw `messaging.Handler`s, JSON-decoding the payload (or a `types.Message`
  envelope) and returning a BadRequest `*core.Error` on decode failure.
  `messaging.NewJSONMessage` builds a `Message` from a typed payload for
  publishing.
- **Request validation** — `httpx.Body[T]` now turns `binding`/`validate` tag
  failures into a BadRequest `*core.Error` carrying a per-field breakdown (field
  name → reason) under the `fields` param, rendered in the 400 response by the
  error middleware. Field names use the json tag (`httpx.UseJSONFieldNames`), and
  `httpx.ValidationError` exposes the translation for manual binders.
- **HTTP client circuit breaker** — `httpx.WithCircuitBreaker(threshold,
  cooldown)` adds a per-client breaker that opens after consecutive server-side
  failures (transport errors and 5xx; 4xx does not count), fails fast with a
  "circuit breaker open" error while open, and admits a single trial request
  after the cooldown. Complements `WithRetry`.
- **Idempotency middleware** — `middleware.Idempotency` stores the response to a
  non-safe request keyed by an `Idempotency-Key` header and replays it for
  retries (marking them with the `Idempotent-Replayed` header), so a retried
  request does not execute twice. Keys are scoped by method + route; only
  responses with status < 500 are stored. Backed by a minimal `IdempotencyStore`
  (Get/Set) interface that `cache.Cache` satisfies directly.

### Changed

- **Internal dependencies pinned to v0.15.0** — every module now requires its
  MicroJet siblings at the matching v0.15.0 release and no longer carries local
  `replace` directives (used only for in-repo development via `go.work`). All
  modules, including the new `otelx`, `outbox`, `testx`, and `gormx/migrate`,
  resolve their dependencies from the module proxy and install with a plain
  `go get`.

## [0.14.0] - 2026-06-14

### Added

- **`host.ServiceSetupper`** / **`core.Setupper`** — new service lifecycle hook for
  the setup phase. A registered service can now perform post-init work that depends
  on other services being connected (migrations, route registration) by
  implementing `Setup(app *App) error` (or the no-arg `core.Setupper`) instead of
  registering a separate `app.Setup` handler. The host runs these hooks after
  `Init` and before the queued `app.Setup` handlers; `host.ServiceSetupper` takes
  precedence over `core.Setupper`. Order across services is unspecified, matching
  the `Start` and `Close` phases.

## [0.13.0] - 2026-06-14

### Added

- **`gormx.Table.Where`** — accumulating WHERE clause that mirrors `gorm.DB.Where`,
  complementing the existing `WhereIf`.
- **`gormx.Table.ListPage`** — returns one page of results using the table's
  accumulated scopes, configured via the fluent `PageOptions[T]` builder
  (`PageSize`, `OrderBy`, `Cursor`, `NextToken`). Supports cursor-based pagination
  without implementing an interface; `Cursor` returns a `[]any` WHERE condition and
  `NextToken` builds the following page's token.

### Changed

- **BREAKING: `gormx.Table.ListAll`** — signature changed from
  `ListAll(ctx, req ListAllRequest)` to `ListAll(ctx, orderBy string)`. Filtering is
  now expressed by chaining `Where`/`WhereIf`/`Preload` on the table. The
  `ListAllRequest` interface has been removed.
- **`gormx.Scoper`** — documented as an optional interface for `ListRequest` only
  (no longer referenced by `ListAll`).

## [0.12.0] - 2026-06-14

### Added

- **`versioninfo.Info`** — new struct with `Get()` and `Print(w io.Writer)` methods
  exposing build version fields as a structured value; suitable for logging, JSON
  serialisation, and writing `key=value` lines to any `io.Writer`.
- **`types/money` constructors** — `New`, `NewFromFloat`, `NewFromInt`, and
  `NewFromString` for constructing `Money` values without direct struct literals.
- **`aws/dynamo.QueryGSIPage`** — GSI pagination method with an optional sort key
  condition alongside the existing `QueryPage`.
- **`aws/dynamo.SKCondition`** — type for expressing sort key conditions on GSI
  queries.
- **`aws/dynamo.Timestamp`** — DynamoDB-compatible timestamp type; `applyTimestamps`
  now handles `Timestamp` in addition to `time.Time`.

### Changed

- **BREAKING: `gormx.Table.Preload`** — signature changed from
  `Preload(fields ...string)` to `Preload(association string, args ...any)`,
  matching GORM's native `Preload` API. Callers that passed multiple associations
  in a single call must now chain `.Preload` calls. Conditional preloads (SQL
  condition string + values, or a `func(*gorm.DB) *gorm.DB`) and
  `clause.Associations` (`"*"`) are now supported.

### Tooling

- CI matrix extended to cover all modules: `messaging/nats`, `gormx/postgres`,
  `gormx/sqlite`, `jsonx`, `tenant`, `versioninfo`. Cache key uses `**/go.sum`
  to handle modules without external dependencies.

## [0.11.0] - 2026-06-10

### Added

- **`jsonx` module** — new `github.com/hatami57/microjet/jsonx` module with JSON
  helpers (previously in `utils`). `aws` and `types` depend on it via a local
  `replace` directive.
- **`host.App.Configure`** — variadic method that calls `ReadConfig` on each
  `Configurable` using the app's shared config reader (replaces `App.LoadConfig`).

### Changed

- **BREAKING: `Configurable` interface** — `LoadConfig(*core.ConfigLoader) error`
  renamed to `ReadConfig(core.ConfigReader) error`. `core.ConfigReader` is now an
  interface (`SetDefault`, `Read`, `ReadMap`, `ReadAll`) instead of the concrete
  `ConfigLoader` struct, so any code implementing `Configurable` must be updated.
- **BREAKING: `core.ConfigReader.ReadAsMap` → `ReadMap`** — the map-read method
  on the `ConfigReader` interface is renamed for brevity.
- **BREAKING: `core.NewConfigLoader` → `core.NewViperConfigReader`** — the
  constructor for the config reader has been renamed to match the new type name.
- **BREAKING: `core.Configure`** — `ConfigLoader.Configure(cfgs...)` is now a
  package-level function `core.Configure(envPrefix, cfgs...)` that creates its own
  reader internally.
- **BREAKING: `host.LoadConfig` → `host.ReadConfig`** — standalone config
  convenience function renamed for consistency.
- **BREAKING: `host.App.LoadConfig` → `host.App.Configure`** — fluent method
  renamed; behavior is unchanged.
- **BREAKING: `App.UseProvider` → `App.WithProvider`** — restored to the
  `With*` naming convention (was briefly `UseProvider` in v0.10.0).
- **BREAKING: `gormx.Table.Remove` → `Table.Delete`** — CRUD vocabulary
  standardised; callers must rename any `Remove` call sites.
- **BREAKING: `AsyncWorker.Go` → `AsyncWorker.Run`** — the background-goroutine
  method is renamed; implement `Run(ctx, app)` instead of `Go(ctx, app)`.
- **BREAKING: `PeriodicWorker.Go`/`GoInterval` → `Run`/`Interval`** — both
  methods on the `PeriodicWorker` interface are renamed.
- **BREAKING: `httpx.Find*` → `httpx.Get*`** — all param/query helpers
  (`FindParam`, `FindUUIDParam`, `FindInt64Param`, `FindInt32Param`, `FindQuery`,
  `FindUUIDQuery`, `FindInt64Query`, `FindTenantID`, `FindUserID`,
  `FindTenantUserID`) are renamed to their `Get*` equivalents.
- **BREAKING: `httpx.Server.LoadConfig` → `Server.ReadConfig`** — satisfies the
  updated `core.Configurable` interface.
- **`core.FixedClock.T` → `FixedClock.Time`** — field renamed for clarity; UTC
  normalization in `NewFixedClock` is also fixed.
- `core.LogConfig`, `core.LogOutputConfig`, and the service lifecycle interfaces
  (`Initer`, `Starter`, `Closer`, `HealthChecker`) are relocated to dedicated
  files (`logger.go`, `service.go`) with no behavioural change.

### Removed

- **`core.ConfigLoader`** (struct) — replaced by the `core.ConfigReader` interface
  and `core.NewViperConfigReader`.
- **`core.ConfigurableFunc`** — removed along with the concrete `ConfigLoader`.
- **`core.NewViper`** — internalized; use `core.NewViperConfigReader` instead.
- **`host.App.LoadConfig`** — replaced by `App.Configure`.
- **`utils` JSON helpers** — moved to the new `jsonx` module.

## [0.10.0] - 2026-06-09

### Added

- **Composable modules** — `host.Module` interface (`Register(app *App) error`) lets
  functionality be bundled behind a single hook and composed into a tree.
  `app.WithModule` / `app.WithModules` install modules with diamond-safe
  deduplication: struct modules install once per type (or per `ModuleKey()` for
  parameterised variants); `host.ModuleFunc` wraps a plain function for
  anonymous, one-off modules that are never deduplicated. Optional
  `host.NamedModule` and `host.KeyedModule` interfaces allow custom display names
  and instance-level dedup keys respectively.
- **`WithMessaging` setup handlers** — `app.WithMessaging(client, setup ...HandlerFunc)`
  now accepts optional route/setup handlers, consistent with `WithHTTPServer`.
- **`UseProvider`** — `app.UseProvider(fn HandlerFunc)` runs a provider function
  and captures any error into the deferred chain (replaces `WithProvider`).
- **`examples/modules`** — runnable three-level diamond dependency tree
  demonstrating recursive registration and shared-module deduplication.

### Changed

- **`host.App.Setup` is now variadic** — `Setup(handler ...HandlerFunc)` accepts
  any number of handlers in one call.
- **`core.NewLogger` gains `forceDebug bool`** — when `true` (wired to
  `cfg.App.Debug`) all log outputs are forced to `debug` level regardless of
  configured level.
- **`ProvidedItem` exported** — `host.ProvidedItem[T, V]` (was unexported
  `providedItem`); callers building custom providers can now type-assert or
  return it directly.
- **`initServices` fixpoint drain** — the service initialization loop runs to a
  fixpoint so services dynamically registered inside another service's `Init`
  are still configured and initialized within the same `Run` call.

### Removed

- **`host.WithProvider`** — replaced by `UseProvider` (no automatic
  `initServices` side-effect; callers control initialization order via the
  normal `Run`/`MustRun` path).
- **`host.ErrDatabaseNotInitialized`** — removed; callers should handle a nil
  `app.DB()` directly or rely on the service lifecycle.

## [0.3.0] - 2026-06-06

### Added

- **Typed extra config** — `host.NewWithExtraConfig[T]` / `host.MustNewWithExtraConfig[T]`
  load the `[extra]` TOML section directly into a caller-supplied type `T`.
  `core.GetExtraConfig[T]` and `core.MustGetExtraConfig[T]` retrieve the typed
  value from `Config.Extra` via a type assertion.
- `core.LoadConfigWithExtra[T]` exposes the same loading logic for callers that
  construct their own `App` rather than using the `host` package.

### Changed

- **BREAKING:** `Config.Extra` is now `any` (was `map[string]any`). Use
  `host.MustNewWithExtraConfig[T]` at startup so the field holds the concrete type,
  then retrieve it with `core.MustGetExtraConfig[T]` / `core.GetExtraConfig[T]`.
- **BREAKING:** Removed `core.GetExtra[T](c, key)`, `core.MustGetExtraConfig[T](c, key)`,
  and all per-type helpers (`MustGetExtraString`, `MustGetExtraInt32`, etc.).
  The new API does a single type-assertion on `Config.Extra` instead of a
  per-key map lookup with reflect-based conversion.
- **BREAKING:** `core.LoadConfig` now requires an `envPrefix string` argument
  (pass `""` to keep the default `APP` prefix). `host.New` / `host.MustNew` are
  unchanged — they pass the prefix internally.

## [0.2.0] - 2026-06-05

First tagged multi-module release. Each module is tagged independently
(`<module>/v0.2.0`); install with the module-prefixed version, e.g.
`go get github.com/hatami57/microjet/httpx@v0.2.0`.

This release contains several **breaking changes** (module/package renames and
interface signatures). See _Changed_ below.

### Added

- **Injectable clock** — `core.TimeProvider` with the `core.UTC` default and a
  `core.FixedClock` for deterministic tests; `host.WithClock(...)` option. Wired
  into the tenant cache via `middleware.WithTenantCacheClock`.
- **Readiness endpoint** — `GET /readyz` runs registered checks
  (`Server.AddReadinessCheck`); the host auto-registers database-ping and
  messaging-health probes.
- **Metrics** — `GET /metrics` (Prometheus) with RED metrics on a private
  registry plus Go runtime/process collectors (`middleware.NewMetrics`).
- **Request correlation** — `middleware.RequestID` reads/generates `X-Request-ID`,
  and the logger middleware exposes a request-scoped `*slog.Logger`
  (`httpx.LoggerFrom(ctx)`); the HTTP client forwards the id upstream.
- **HTTP client retries** — `httpx.WithRetry` adds exponential backoff with
  jitter on idempotent methods (configurable), honoring the context.
- **CORS** — `middleware.CORS` / `DefaultCORSConfig` (opt-in).
- **JWT auth** — `middleware.JWT` (HMAC or custom `Keyfunc`, explicit accepted
  algorithms), with `middleware.JWTClaimsFromContext`.
- **Rate limiting** — `middleware.RateLimit` (per-client token bucket, 429 +
  `Retry-After`, idle eviction).
- **Cache module** — new `github.com/hatami57/microjet/cache` with a `Cache`
  interface, `RedisCache`, `MemoryCache`, and `GetJSON`/`SetJSON` helpers.
- **Named multi-database config** — `[databases.<name>]` config tables and
  `host.WithDatabasesFromConfig`.
- **Lifecycle** — `host.WithShutdownTimeout`, `host.WithMessagingClient`.
- **Config** — `core.ConfigViper` exposes the shared config loader so modules can
  load their own typed sections.

### Changed

- **BREAKING:** renamed module/package `http` → `httpx`
  (`github.com/hatami57/microjet/httpx`).
- **BREAKING:** renamed module/package `postgres` → `gormx`
  (`github.com/hatami57/microjet/gormx`); it is generic GORM CRUD + cursor
  pagination usable with any `*gorm.DB`.
- **BREAKING:** `messaging.Client` now takes `context.Context` and a `Message`
  carrying a `Headers` map: `Publish(ctx, Message)` and
  `Subscribe(ctx, subject, Handler)`.
- **BREAKING:** `httpx.Body[T]` returns `T` instead of `*T`.
- **BREAKING:** `httpx` `(*QueryParamBase).BindQueryParams` now returns `error`
  instead of silently swallowing parse failures.
- **BREAKING:** removed `AWSConfig` from `core.Config`; AWS config now lives in
  the `aws` module (`aws.Config`/`aws.LoadConfig`). The `[aws]` TOML keys and
  `APP_AWS_*` env overrides are unchanged.
- **BREAKING:** unexported `gormx.Table.DB` (use the constructors / `CloneWithTx`).
- **BREAKING:** removed `utils.IgnoreError`.
- `app.debug` now defaults to `false` (was `true`).
- `messaging.Disconnect` surfaces the NATS `Drain` error; `messaging` adds an
  optional `HealthChecker`.

### Fixed

- `aws.S3DownloadFiles` no longer leaks goroutines (uses `errgroup` with a worker
  limit) and streams via `io.Copy`.
- `aws.SQSSendMessage` returns an error when the client is unconfigured instead
  of reporting success.
- Background workers recover panics instead of crashing the process
  (`host`), start in a deterministic order, and dedupe DI-registered workers.
- Periodic workers use a single `time.Ticker` (no per-tick timer allocation).
- The HTTP server reports a clean shutdown as well as errors during `Run`.
- `App.Close` is bounded by a shutdown timeout.
- `core` config loading uses `path/filepath`; the logger uses a literal `Z`.
- Various idiomatic cleanups (`interface{}` → `any`, checked type assertions,
  `versioninfo.GoVersion` via `runtime.Version`).

### Tooling

- Added `staticcheck` to CI and a `make staticcheck` target.
