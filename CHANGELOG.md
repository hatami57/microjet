# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims to
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
