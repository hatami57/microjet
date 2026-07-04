# MicroJet — Improvement Backlog

Findings from a full-repo review (2026-07-02). Ordered by priority within each
section. Same workflow as `ROADMAP.md`: each top-level item should be its own
commit (Conventional Commits; no AI attribution). Check items off as they land.

Legend: **[P1]** fix soon (defect or high-value/low-cost) · **[P2]** next
feature wave · **[P3]** nice to have / larger design work.

---

## 1. Defects / doc drift (fix first)

### 1.1 [P1] README documents APIs that don't exist  `docs` — **done**

The README's front-page examples reference the pre-rename API surface. Nothing
in `core` re-exports these, so every snippet below fails to compile for a new
user.

- [x] **Error Handling section** (README.md:253–275): uses `core.NewNotFoundError`,
  `core.ErrBadRequest`, `core.IsNotFoundError`, `core.ErrNotFound` (also
  README.md:16). Real package is `core/errorx`. Fix imports/prose to
  `errorx.NewNotFoundError(...)`, `errorx.ErrBadRequest`, `errors.Is(err, errorx.ErrNotFound)`.
- [x] **Config API** (README.md:80–98, 234–251): uses `core.ConfigLoader`,
  `core.Configurable`, `LoadConfig(l *core.ConfigLoader)`, `l.UnmarshalKey`,
  `app.LoadConfig(&cfg)`. Real API: implement `configx.Configurable` with
  `ReadConfig(l configx.Reader) error` using `l.Read(section, &cfg)`, and load
  via `app.Configure(&cfg)`.
- [x] **Multi-database API** (README.md:20, 164–167): uses `gormx.NamedModule(name, driver)`
  and `gormx.NamedDB(app, name)` — neither exists; the real API is
  `gormx.Module(driver, name)` and `gormx.Of(app, name)` (gormx/module.go:44,80).
- [x] **Cached tenant store** (README.md:170–190): uses
  `middleware.NewCachedTenantStore(dbStore, 5*time.Minute)` — no such function;
  the real API is `tenant.NewCachedStore(store, ttl, opts...)` in `core/tenant`
  (returns `*tenant.CachedStore` with `Invalidate(id)`/`Clear()`).
- [x] **Stale comment** host/host.go:70: says "call app.LoadConfig after
  construction" — no such method; should say `app.Configure`.
- [x] **Architecture diagram** (README.md:490–507): shows the old single-module
  layout (`utils → types → aws` as one tree, `host` importing everything).
  Redraw to match the real multi-module graph: `core` at the bottom; `host`
  depends only on `core`; satellite modules (`httpx`, `gormx`, `messaging`,
  `cache`, `otelx`, `outbox`, `aws`, `testx`) each depend on `core` (+ `host`
  for their `Module()`), never on each other except `outbox → messaging/gormx`.
- [x] While in there: sweep all fenced code blocks in README + `docs/` for the
  same drift (`host.WithAWS`, `host.WithTracing`, `host.WithSubscriber` era
  names appear in issues.md history; make sure none leak into user-facing docs).

### 1.2 [P1] CI job that compiles README snippets  `ci`

Doc drift recurs unless it breaks the build. Add a CI job that extracts fenced
` ```go ` blocks from README.md (and `docs/*.md`), writes each into a throwaway
module under a temp dir with `replace` directives pointing at the local
modules, and runs `go build ./...`. Skip blocks marked ` ```go ignore `.
A ~40-line shell/Go script in `scripts/` is enough; wire it as a `docs` job in
ci.yml.

### 1.3 [P1] Fix or remove the "Packages" table drift check  `docs` — **done** (folded into 1.1)

README.md:36 describes `core` subpackages including `errorx` — correct — but
README.md:16 says "(`errors.Is(err, core.ErrNotFound)`)". One-line fix; listed
separately so it isn't missed if 1.1 is split across commits.

---

## 2. Feature gaps

### 2.1 [P2] gRPC module (`grpcx`)  `feat` (new module)

Biggest gap for a framework that says "microservice". Internal service-to-service
traffic in Go shops is predominantly gRPC, and every needed building block
already exists as HTTP middleware — it needs interceptor twins.

Scope for a first release:

- New module `grpcx` (own `go.mod`, like `httpx`), `grpcx.Module()` +
  `grpcx.Of(app)` returning a managed `*grpc.Server`.
- Config section `[grpc]`: `host`, `port`, `debug`; lifecycle identical to
  `httpx.Server` (Init builds, Start serves in a goroutine exposing `ErrCh()`,
  Close does `GracefulStop` bounded by the app shutdown timeout).
- Unary + stream server interceptors mirroring the HTTP stack, in order:
  recovery (panic → `codes.Internal`, log stack), request-id (accept/generate
  and put in ctx, echo in trailer), otel (`otelgrpc` stats handler, no-op until
  `otelx.Module()` is on — same pattern as httpx tracing), logging (per-RPC
  slog with code/duration/request_id), and an errorx→status translator mapping
  the 6 error categories to gRPC codes (BadRequest→InvalidArgument,
  NotFound→NotFound, Business→FailedPrecondition, Unauthorized→Unauthenticated,
  Forbidden→PermissionDenied, Internal→Internal).
- Register `grpc_health_v1` health service (wired to the same readiness checks
  as `/readyz`) and reflection when `debug = true`.
- Client side: a dial helper that installs the matching client interceptors
  (request-id propagation, otel) — parity with what `httpx.Client` does for
  HTTP.

### 2.2 [P1] Runtime + custom metrics  `feat(httpx)` / `feat(gormx)` / `feat(cache)`

`/metrics` currently serves only HTTP RED metrics on a private registry
(httpx/middleware/metrics.go:26) with **no accessor**, so an app cannot add its
own counters to the endpoint it already exposes.

- [ ] Register `collectors.NewGoCollector()` and
  `collectors.NewProcessCollector(...)` on the registry (goroutines, GC pause,
  RSS — the first graphs anyone looks at in an incident).
- [ ] Expose the registry: `func (m *Metrics) Registry() *prometheus.Registry`
  plus a convenience `httpx.Of(app).Metrics()` so app code can
  `MustRegister` custom collectors.
- [ ] gormx: export DB pool stats via `prometheus.NewGaugeFunc` over
  `sql.DBStats` (open/in-use/idle/wait-count/wait-duration), one set per named
  DB, registered when both gormx and httpx modules are present (resolve the
  metrics service via DI in `Init`, skip silently if absent).
- [ ] cache: hit/miss/error counters per driver, same opt-in wiring.
- Note: keep the registry private-by-default behavior (no global
  `prometheus.DefaultRegisterer`) — that was the right call; just add the
  accessor.

### 2.3 [P1] `/debug/pprof` endpoints  `feat(httpx)` — **done**

- [x] Mount `net/http/pprof` under `/debug/pprof/*` via `gin.WrapF/WrapH`
  (index, cmdline, profile, symbol, trace + the named profiles), gated behind
  `[http] debug` — the same gate as Swagger. No separate flag needed.

### 2.4 [P1] HTTP server hardening  `feat(httpx)` (config-compatible) — **done**

httpx/server.go hardcoded timeouts and omitted two protections; now all
configurable in `[http]` with the previous values as defaults.

- [x] Add `ReadHeaderTimeout` (slowloris exposure — Go's `ReadTimeout` alone
  does cover it, but only because it's set; if a user config later allows
  `readTimeout = 0` the header path becomes unbounded). Default 5s.
- [x] Add `MaxHeaderBytes` (default 1 MiB, configurable).
- [x] Make all timeouts configurable in `[http]` with current values as
  defaults: `readTimeout = "10s"`, `writeTimeout = "10s"`, `idleTimeout = "60s"`,
  `readHeaderTimeout = "5s"`. Uses `time.Duration` mapstructure decoding —
  viper's UnmarshalKey enables StringToTimeDurationHookFunc by default (verified),
  so defaults are set as duration strings and env overrides (e.g.
  `APP_HTTP_READTIMEOUT=15s`) decode too.
- [x] TLS: `certFile`/`keyFile` in `[http]`; when both set, `serve()` uses
  `ListenAndServeTLS`. (mTLS/client-CA can wait.)
- [x] New middleware, all opt-in like CORS:
  - `middleware.BodyLimit(maxBytes)` — reject > limit with 413 (declared
    Content-Length rejected up front; chunked/undeclared bodies capped via
    `http.MaxBytesReader`; errorx has no 413 category so it's emitted directly).
  - `middleware.Timeout(d)` — per-request deadline; on expiry flushes 503
    immediately and cancels the request ctx. Race-free: the handler runs on the
    request goroutine and a watcher goroutine flushes the 503, so gin.Context is
    never touched concurrently (unlike the c.Next()-in-goroutine + c.Abort()
    pattern, which races c.index). Buffers the response, so not for streaming; the
    handler is not killed, only its ctx cancelled.
  - `middleware.SecureHeaders(cfg)` — `X-Content-Type-Options: nosniff`,
    `X-Frame-Options: DENY`, `Referrer-Policy`, optional CSP, optional HSTS (only
    emitted on TLS requests).
  - gzip: README recommends `gin-contrib/gzip` instead of wrapping it.

### 2.5 [P1] Kubernetes-aware shutdown (readiness flip before drain)  `feat` — **done**

Previous `Run()` shutdown: signal → cancel workers → `Close()`. During a rolling
deploy, kube-proxy/endpoints lag means a few requests still land on the
terminating pod and get connection-refused. `Run()` now flips readiness and waits
before draining.

- [x] Add `Server.SetReady(ready bool)`; when false, `/readyz` returns 503
  `{"status":"shutting-down"}` without running the checks.
- [x] In the host shutdown path (before cancelling workers / closing services):
  `beginShutdown()` resolves every service implementing the new
  `core.ReadinessToggler` interface (`SetReady(bool)`), flips it off, then waits
  `[app] shutdownDelay` (default `"0s"` — zero keeps current behavior and skips
  the wait entirely when nothing toggles; README documents ~5s on Kubernetes with
  a matching `terminationGracePeriodSeconds`).
- [x] `/health` (liveness) stays 200 throughout — only readiness flips,
  otherwise kubelet restarts the pod mid-drain.

### 2.6 [P2] Outbox: safe concurrent relays  `feat(outbox)`

relay.go:19–20 documents "concurrent relays may double-publish" but the
framework offers no coordination tool. Preferred fix — `FOR UPDATE SKIP LOCKED`:

- In `PublishPending`, wrap the pass in a transaction and select the batch with
  `.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})`.
  Concurrent relays then partition the pending set instead of racing — the
  textbook multi-replica outbox. Marking `published_at` happens inside the same
  tx.
- Dialect caveat: SKIP LOCKED is Postgres/MySQL 8+; SQLite has no row locks.
  Gate on dialect (`db.Dialector.Name()`): on sqlite keep today's lock-free
  path and document that sqlite deployments are single-instance anyway.
- Trade-off to note in the commit: holding the tx across `publisher.Publish`
  lengthens lock hold time; batch size already bounds it. Alternative
  (claim-then-publish with a `claimed_at` column) is more complex — not worth
  it at this scale.
- Optional follow-up: a general `lockx` helper (Postgres
  `pg_try_advisory_lock` + Redis `SET NX PX`) for users running periodic
  workers on multiple replicas — a common concern beyond the outbox. Design it
  only if demand shows up; SKIP LOCKED removes the outbox's need for it.

### 2.7 [P2] Cache: stampede protection + atomic ops  `feat(cache)`

The interface covers plain get/set; daily-driver operations are missing:

- `GetOrSetJSON[T](ctx, c, key, ttl, fill func(ctx) (T, error)) (T, error)` —
  package-level generic like GetJSON. Guard the fill with
  `golang.org/x/sync/singleflight` (per-process; note in the doc comment that
  cross-replica stampedes need a distributed lock, see 2.6 follow-up).
- Extend `Cache` (breaking for external implementors — batch with other
  breaking changes, or add as a separate optional interface
  `cache.Atomic` + capability check, which is non-breaking; prefer the
  optional-interface route):
  - `Increment(ctx, key string, delta int64, ttl time.Duration) (int64, error)`
    (Redis INCRBY+EXPIRE NX; memory: mutex-guarded).
  - `SetNX(ctx, key string, value []byte, ttl time.Duration) (bool, error)`.
  - `GetMulti(ctx, keys []string) (map[string][]byte, error)` (Redis MGET).

### 2.8 [P2] Reusable retry/backoff package  `feat(core)`

Retry logic exists only inside `httpx.Client` (`WithRetry`). DB callers,
messaging publishers, and startup connection loops all rewrite it.

- New `core/retryx` (subpackage of core, no new go.mod):
  `retryx.Do(ctx, policy, func(ctx) error) error` and a generic
  `retryx.DoValue[T]`. Policy: max attempts, initial/max backoff, multiplier,
  full jitter, `RetryIf func(error) bool`. Honors ctx cancellation between
  attempts.
- Refactor `httpx.Client`'s retry internals onto it (behavior-identical;
  keep `WithRetry`'s public surface unchanged).

### 2.9 [P2] Secrets from files (`*_FILE` convention)  `feat(core)`

Docker/K8s mount secrets as files; the standard convention is
`APP_DATABASE_PASSWORD_FILE=/run/secrets/db_password` meaning "read the value
from this file". In `applyEnvOverrides` (core/configx — the env shim), when a
key `X_FILE` is present and `X` is not, read the file (trim one trailing
newline) and treat it as `X`'s value. Document under Configuration. No config
struct changes needed.

### 2.10 [P2] JWKS support for JWT middleware  `feat(httpx)`

`JWTConfig.Keyfunc` is the extension point but every OIDC integrator rebuilds
the same JWKS fetcher. Add `middleware.NewJWKSKeyfunc(url string, opts...)`:
fetches the JWKS, caches by `kid`, refreshes on unknown-kid (rate-limited, e.g.
min 1/min) and on a TTL (default 1h), thread-safe. Either hand-roll (~150
lines over `crypto/*` + `encoding/json`) or wrap `github.com/MicahParks/keyfunc`
— decide by dependency-weight policy; hand-rolling fits this repo's style.

### 2.11 [P3] Config validation hook  `feat(core)`

After `ReadConfig`, if the config struct implements
`interface{ Validate() error }`, call it and fail startup with the wrapped
error. One `if v, ok := cfg.(Validator); ok` in `configx.Read` /
`app.Configure`. Cheap, catches the "empty DSN discovered at first query"
class at boot.

### 2.12 [P3] NATS JetStream  `feat(messaging)`

Core NATS pub/sub drops messages while a consumer is down, which undercuts the
outbox's at-least-once promise *end to end* (the relay guarantees delivery to
the broker, not to the consumer). JetStream adds durable consumers, acks,
redelivery, and DLQ via max-deliveries. Larger design: needs stream/consumer
config surface, `messaging.Subscribe` ack semantics (explicit ack on handler
nil-return), and a decision on whether it's a new `messaging/jetstream` module
(consistent with the driver pattern) — sketch that before coding. Do after 2.6.

---

## 3. Naming & structure

### 3.1 [P1] Rename `issues.md` → `ROADMAP.md`  `docs` — **done**

On a public repo, root-level `issues.md` reads as a known-bugs list; it's
actually a well-kept roadmap/changelog of phases. Renamed via `git mv`, header
rewritten to point at IMPROVEMENTS.md for new work, and the "Deferred earlier
(judgment calls)" section reframed as "Design decisions (deliberately not
done)".

### 3.2 [P3] Break up `core/utils`  `refactor` (breaking — batch with next major wave)

`core/utils` mixes JSON helpers, struct/map converters, env access, and disk
helpers — no cohesion, and `utils` is the first thing reviewers flag in a
public API. The sibling `jsonx` shows the target pattern. Plan: `envx` (env),
`fsx` (disk), converters either join `jsonx` or become `convx`. Keep
deprecated aliases in `core/utils` for one release cycle
(`// Deprecated: use envx.X`), then remove. Do not do this standalone — batch
with 2.7's interface change or the next planned breaking release.

### 3.3 No action: module naming is fine

The `-x` suffix is applied exactly where stdlib/ecosystem collision exists
(`httpx`, `gormx`, `otelx`, `configx`, `errorx`, `logx`, `jsonx`, `testx`) and
omitted where it doesn't (`cache`, `outbox`, `host`, `messaging`, `aws`).
`Of`/`Lookup`, `Find*`/`Get*`, `Must*` conventions are coherent across modules.
Keep enforcing in review; nothing to change.

---

## 4. Maintainability / CI

### 4.1 [P1] `govulncheck` in CI  `ci`

Table stakes for a public framework — its CVE exposure is its users' CVE
exposure. Add a job running `golang.org/x/vuln/cmd/govulncheck ./...` per
module (reuse the discover matrix), on PRs **and** on a weekly `schedule:`
trigger (new CVEs land against unchanged code).

### 4.2 [P1] `golangci-lint` in CI + committed config  `ci`

The Makefile has a `lint` target but CI only enforces gofmt + staticcheck +
tests, so the lint bar is optional. Commit a `.golangci.yml` (start minimal:
govet, staticcheck, errcheck, revive, misspell — tune from there) and add the
job (official `golangci/golangci-lint-action`, per-module via the matrix).
Fix or `nolint`-annotate existing findings in the same PR so it lands green.

### 4.3 [P1] Auto-discover modules in the Makefile  `build` — **done**

Makefile:1 hand-maintained `MODULES` while ci.yml auto-discovers via
`find . -name go.mod`. Now discovered with the same find (excluding
`examples/`, which are never released or tagged). Verified: release.sh uses
MODULES only for tag names and the post-tag verify loop, both order- and
set-compatible with the discovered list, so no `RELEASE_MODULES` split was
needed.

### 4.4 [P2] Slim the README; move depth to `docs/` + `doc.go`  `docs`

At ~500 lines the README is a reference manual, which is why it drifted (§1.1).
Keep: features list, install, quick start, one full example, links. Move
per-topic depth (aggregates/projections, atomic & guarded updates, tenant
caching, modules deep-dive, pagination) into `docs/<topic>.md` and/or package
`doc.go` files — pkg.go.dev renders those next to the code and they can't
reference a wrong import path silently. Combine with 1.2 so what remains is
compile-checked.

### 4.5 [P2] `SECURITY.md`  `docs`

Private disclosure channel (email software.apan@gmail.com or GitHub private
vulnerability reporting — enable it in repo settings), supported-versions
statement (latest minor), response-time expectation. GitHub surfaces the file
in the Security tab; its absence gets noticed on a library shipping JWT and
rate-limit middleware.

---

## Suggested order of execution

1. §1.1 + 1.3 README fixes, §3.1 rename, §2.3 pprof, §4.3 Makefile — one small PR each, all P1, no design needed.
2. §2.4 server hardening + §2.5 readiness flip (related surface, httpx + host).
3. §4.1 govulncheck + §4.2 golangci-lint + §1.2 README-snippet CI (one CI-focused PR).
4. §2.2 metrics (httpx accessor + collectors first; gormx/cache emitters after).
5. §2.6 outbox SKIP LOCKED, §2.7 cache ops, §2.8 retryx, §2.9 secrets-file, §2.10 JWKS.
6. §2.1 grpcx (design sketch first), §2.12 JetStream (after 2.6), §3.2 utils split + any batched breaking changes, §4.4 README slim-down, §4.5 SECURITY.md, §2.11 validation hook.
