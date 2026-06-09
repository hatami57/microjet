# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims to
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
