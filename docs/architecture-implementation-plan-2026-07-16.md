# Architecture Implementation Plan — 2026-07-16

This is the actionable subset of `docs/architecture-review-2026-07-13.md`, filtered
to the items verified against the source and judged worth implementing, with
concrete implementation guidance. Work through the tasks **in order** — later
tasks build on earlier ones. Each task is independently committable.

Items from the review that are deliberately **not** in this plan (do not
implement them): composition presets (review §4), the `grpcx` module (§7),
messaging capability interfaces (§8), splitting the `core` module (§9), and the
secret-provider/validation-framework parts of §10. See "Out of scope" at the end
for the reasoning, so nobody re-adds them by accident.

## House rules (apply to every task)

- This is a multi-module repo and stays that way: 8+ published modules linked by
  `go.work` for local dev. Never consolidate modules.
- Test entry point is `make test` (per-module); root-level `go test ./...` is
  not supported. For a single module: `cd <module> && go test ./...`.
- Commit messages use Conventional Commits (`feat(host): ...`, `fix(outbox): ...`,
  `ci: ...`). Breaking changes get a `!` and a `BREAKING CHANGE:` footer.
- `IMPROVEMENTS.md` is the active backlog. Where a task below overlaps an entry
  there (noted per task), tick the relevant checkboxes **in the same commit**.
- Keep new code additive. None of these tasks requires breaking an existing
  public API; if you find yourself breaking one, stop and reconsider the design.
- After each task, run the affected module's tests plus `make vet` before
  committing.

---

## Task 1 — Tidy every module's `go.mod` and enforce it in CI `[P1]`

### Problem (verified)

Most satellite modules carry large blocks of `// indirect` requirements they
cannot reach from their import graphs, including sibling microjet modules they
never import:

- `messaging/go.mod` directly imports only `core` and `host`, yet requires the
  AWS SDK (~20 modules), `gin`, `go-redis`, `mongo-driver`, `quic-go`, `gorm`,
  and sibling modules `microjet/{aws,cache,gormx,httpx,otelx,outbox}` — all
  marked indirect.
- `httpx/go.mod` requires `microjet/{aws,cache,gormx,messaging,otelx,outbox}`
  plus the AWS SDK, redis, mongo-driver — none imported by `httpx` code.
- `cache`, `otelx`, `outbox`, and `aws` have the same class of pollution.
- `host/go.mod` and `gormx/go.mod` are clean — proof of what the others should
  look like.

Consequences for consumers: bloated `go.sum`, unnecessary minimum-version
constraints, and `govulncheck` findings for code they never build (the repo's
own CI already hit one via `quic-go`, see IMPROVEMENTS.md §4.1).

### Implementation

1. For each module listed in `go.work` (the Makefile auto-discovers the same
   list), run tidy in isolation from the workspace:

   ```sh
   cd <module> && GOWORK=off go mod tidy
   ```

   `go mod tidy` normally ignores the workspace, but `GOWORK=off` makes the
   isolation explicit and reproducible. Include the nested modules
   (`gormx/migrate`, `gormx/postgres`, `gormx/sqlite`, `messaging/nats`) and the
   `examples/*` modules.

2. Review the diffs. Expected outcome: the indirect sibling-module requires and
   unreachable third-party requires disappear; direct requires are untouched.
   If tidy tries to **remove a direct require that is actually imported**, or
   fails to resolve a version, stop and investigate rather than forcing it.

3. Safety check on the release process before committing:
   `scripts/release.sh` bumps internal `hatami57/microjet/... vCURRENT` requires
   to `vNEXT` by **string replacement of existing require lines only** (see the
   comment at the top of the script). Removing requires therefore does not break
   it — there are simply fewer lines to bump. Confirm by reading the script's
   step 3 after making the changes.

4. Add a `tidy` job to `.github/workflows/ci.yml` (alongside the existing
   `fmt`/`test`/`govulncheck`/`lint`/`docs` jobs, reusing the `discover` matrix
   job the others use): per module, run `GOWORK=off go mod tidy` and fail on
   `git diff --exit-code -- '**/go.mod' '**/go.sum'`.

5. Add a second small CI job: **external consumer build**. Create a throwaway
   module in a temp dir (outside the repo, so `go.work` cannot apply), with a
   `main.go` that imports and minimally uses `host`, `httpx`, and `gormx` +
   `gormx/sqlite`, with `replace` directives pointing at the checkout paths.
   `go mod tidy && go build ./...` must succeed. This is the regression guard
   that keeps Task 1 fixed and satisfies the CI half of review §11. Model it on
   `scripts/check-doc-snippets.sh`, which already builds throwaway modules.

### Acceptance criteria

- `messaging/go.mod` no longer requires the AWS SDK, gin, redis, mongo-driver,
  quic-go, or any sibling microjet module beyond `core` and `host`.
- Every module builds and its tests pass in isolation (`GOWORK=off go test ./...`
  from the module dir) and via `make test`.
- CI fails if any `go.mod`/`go.sum` becomes untidy again.
- CI builds an external consumer module without the workspace.

Commit as `fix(deps): ...` / `ci: ...` (two commits are fine: the tidy sweep,
then the CI jobs).

---

## Task 2 — Explicit runtime API: `Start(ctx)` / `Wait()` / `Shutdown(ctx)` `[P1]`

### Problem (verified)

`App.Run()` (`host/host.go:165`) owns SIGINT/SIGTERM handling, creates its own
root `context.Background()`, and blocks until termination. There is no way to
run an App inside a monolith, CLI, test suite, or supervisor that already owns
cancellation. Tests currently cannot start and stop an App without signals.

### Current structure to preserve

`Run()` today executes, in order: `initServices` → `setupServices` →
`runSetups` → `startServices` → `startWorkers(ctx)` → select on
(signal | merged `fatalErrCh()`) → `beginShutdown()` (readiness flip +
`App.ShutdownDelay` drain) → cancel worker ctx → `workerWg.Wait()` → `Close()`.
`Close()` is `sync.Once`-guarded and bounds shutdown with `shutdownTimeout`.
All of that behavior must survive unchanged under `Run()`.

### Implementation

Add to `host.App` (new file `host/runtime.go` is a reasonable home):

```go
// Start brings the app fully up — init, setup, start, workers — without
// blocking. The supplied ctx becomes the root of the app's worker context;
// cancelling it begins graceful shutdown. Returns the first startup error.
func (a *App) Start(ctx context.Context) error

// Wait blocks until the app begins stopping: the Start context is cancelled,
// a service's background loop exits (ErrSource), or Shutdown is called. It
// returns the fatal service error, if any.
func (a *App) Wait() error

// Shutdown gracefully stops the app: flips readiness, waits the configured
// drain delay, cancels workers, waits for them, and closes services. The ctx
// deadline bounds the whole sequence in addition to WithCloseTimeout. Safe to
// call more than once.
func (a *App) Shutdown(ctx context.Context) error
```

Internals:

- `Start` stores a derived `context.WithCancel(ctx)` + cancel func + the
  `*sync.WaitGroup` from `startWorkers` + the merged `fatalErrCh()` on the App
  (new fields). It runs the same four phases `Run` runs today, with the same
  wrapped error messages, and calls `Close()` on failure exactly as `Run` does.
- `Wait` selects on the stored worker context's `Done()`, the fatal channel,
  and an internal "shutdown requested" channel. First fatal error wins and is
  remembered so repeated `Wait` calls return the same value.
- `Shutdown` runs `beginShutdown()`, cancels the worker context, waits on the
  worker WaitGroup (bounded by the caller ctx), then `Close()`. Guard with its
  own `sync.Once` so double-shutdown is safe; `Close` is already once-guarded.
- While migrating `beginShutdown` (`host/host.go:227`): replace the raw
  `time.Sleep(delay)` with a ctx-aware wait (`select` on `time.After(delay)` /
  `ctx.Done()`) so a `Shutdown` deadline can cut the drain short. `Run()`
  passes a background ctx, preserving today's behavior.
- Reimplement `Run()` as: `Start(context.Background())`, then select on
  `notifySignals()` vs `Wait()`, then `Shutdown` with the existing timeout
  semantics. Its observable behavior — log lines, error wrapping
  ("initializing services: ...", etc.), signal handling, return value — must
  not change. `MustRun` stays as-is.

### Tests

In `host`, add tests that (no signals anywhere):

- `Start` + `Shutdown` runs a registered worker and stops it; worker ctx is
  cancelled and the worker observed to exit before `Shutdown` returns.
- Cancelling the ctx passed to `Start` unblocks `Wait()`.
- A fake `ErrSource` service delivering an error unblocks `Wait()` and the
  error is returned.
- `Shutdown` respects its ctx deadline when a worker refuses to exit.
- Calling `Shutdown` twice (and `Shutdown` after `Run`-style close) is safe.
- A `core.ReadinessToggler` fake is flipped to not-ready during `Shutdown`.

### Acceptance criteria

- App can be started and stopped in-process without signals.
- `Run()` behavior is byte-for-byte compatible (same logs, same errors).
- All existing `host` tests pass unchanged.

Overlap: this is the foundation review §3 asks for. Commit as
`feat(host): add Start/Wait/Shutdown runtime API`.

Follow-up in the same task: update the README's lifecycle section (and any
`docs/` page describing `Run`) — the doc-snippet CI job will catch drift.

---

## Task 3 — Injectable config reader: `host.WithConfigReader` `[P2, small]`

### Problem (verified)

`host.New()` hardcodes `configx.NewViperConfigReader(a.envPrefix)`
(`host/host.go:79`), which does filesystem TOML discovery. An embedded app
(Task 2's audience) or a test cannot supply configuration programmatically.
The seam already exists: `App.configReader` holds a `configx.Reader`
(interface at `core/configx/config.go:7`) and everything downstream —
`Configure`, per-service `ReadConfig` in `initServices` — already goes through
that interface.

### Implementation

```go
// WithConfigReader injects the configuration source, bypassing the default
// TOML file discovery. Use it to embed the app in a host process that already
// owns configuration, or to supply fixed config in tests.
func WithConfigReader(r configx.Reader) Option {
    return func(a *App) { a.configReader = r }
}
```

In `New()`, only construct the Viper reader when `a.configReader == nil`.
Everything else is untouched. Note in the option's doc comment the precedence
that results: whatever the injected Reader returns is authoritative — the
default env-var override shim only applies to the Viper reader.

Check `core/configx` and `testx` for an existing in-memory/static `Reader`
implementation; if none exists, add a minimal map-backed one to `configx`
(exported, so consumers can use it too) and use it in the tests.

### Tests

- `host.New(host.WithConfigReader(staticReader))` builds an App whose
  `App.Config` reflects the injected values, with no config file on disk
  (run in a temp working directory to prove no filesystem discovery happened).
- `Configure` populates a service config struct from the injected reader.

Commit as `feat(host): allow injecting the config reader`. This is the only
part of review §10 to implement now.

---

## Task 4 — Replica-safe outbox relay via `FOR UPDATE SKIP LOCKED` `[P1]`

### Problem (verified)

`Relay.PublishPending` (`outbox/relay.go:103`) selects pending rows with no
locking; the type's own doc comment warns that concurrent relays may
double-publish. Multiple replicas is the normal production topology, so today
every multi-replica deployment of `outbox.Module` double-publishes under load.
This is IMPROVEMENTS.md §2.6 — implement it per this spec and tick it there.

### Design

Claim batches with row locks inside a transaction on dialects that support it:

- Inside `PublishPending`, when the dialect supports it, run the whole pass in
  one transaction: `SELECT ... FOR UPDATE SKIP LOCKED` (gorm:
  `Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})`), then
  publish and mark rows within that transaction. Two relays then partition the
  pending set instead of both reading it.
- Dialect gating: `db.Dialector.Name()` — `"postgres"` and `"mysql"` get the
  locking path; anything else (notably `"sqlite"`) keeps the current unlocked
  path. Document on `Relay` and on `outbox.Module` that SQLite (and other
  dialects without SKIP LOCKED) still requires a single relay instance.
- Holding the transaction while publishing is acceptable at the default batch
  size of 100; note it in the doc comment and recommend smaller `BatchSize`
  values for slow brokers. Do **not** build the claim-column
  (claimed_at/claimed_by lease) design now — record it as the escape hatch in
  the doc comment if lock-hold time ever becomes a problem.
- Failure semantics must not regress: `recordFailure` (attempts, last_error,
  quarantine via `failed_at`) and the "published but not marked" at-least-once
  window keep working inside the transaction. Note the semantic shift and
  document it: with the transactional path, a crash after publish but before
  commit rolls back the `published_at` mark, so at-least-once redelivery still
  applies — same guarantee as today, different mechanism.

Watch the plumbing: `Relay` uses `gormx.Table[Message]`; check whether `Table`
supports running under a caller-supplied transaction (a `WithTx`/`Tx`-style
method or constructing `gormx.NewTable` over the tx `*gorm.DB`). Constructing a
per-pass `Table` over the transaction handle is the simplest route.

### Tests

- Unit: dialect selection logic (locking clause applied for postgres/mysql,
  absent for sqlite) — assertable via gorm's DryRun session by inspecting the
  generated SQL, no real database needed.
- Existing sqlite-backed relay tests must pass unchanged (they exercise the
  fallback path).
- Integration (the real proof): two concurrent relays over one Postgres
  database, N pending messages, a publisher that records message IDs — assert
  every ID is published exactly once and both relays made progress. Gate it
  behind an env var DSN (e.g. `MICROJET_TEST_POSTGRES_DSN`), skipping when
  unset; check `testx` and the `gormx/postgres` tests first and reuse whatever
  Postgres-test convention already exists rather than inventing a new one. If
  a convention exists in CI, wire the test into it; if not, the env-gated test
  plus local instructions in the test file header is enough for this task.

### Acceptance criteria

- Two concurrent relays on Postgres never publish the same message twice.
- SQLite keeps current behavior; the single-relay requirement is documented on
  both `Relay` and `Module`.
- `Relay`'s doc comment no longer warns that production topologies are unsafe;
  it states the dialect-dependent guarantee instead.
- IMPROVEMENTS.md §2.6 checkboxes ticked in the same commit.

Out of scope for this task (defer, keep in IMPROVEMENTS.md): retry
backoff/jitter, relay lag/failure metrics, operator replay tooling. The
locking fix must not wait for them.

Commit as `feat(outbox): make the relay safe across replicas via SKIP LOCKED`.

---

## Task 5 — Expose the metrics registry for custom collectors `[P1, small]`

### Problem (verified, narrower than the review claims)

The review says runtime/process metrics are absent — that is **already done**:
`middleware.NewMetrics` (`httpx/middleware/metrics.go:39-44`) registers
`collectors.NewGoCollector()` and the process collector. The remaining real gap:
the registry is a private field with no accessor, and `Server.metrics`
(`httpx/server.go:62`) is also private — so application code has no way to add
a custom collector to what `/metrics` serves.

### Implementation

- `func (m *Metrics) Registry() *prometheus.Registry` on
  `httpx/middleware.Metrics`, doc comment stating it returns the live registry
  backing `/metrics` and that callers may `MustRegister` their own collectors
  (typically during a `Setup` hook, before serving).
- `func (s *Server) MetricsRegistry() *prometheus.Registry` on `httpx.Server`
  delegating to it, since apps hold the `*Server` (via `httpx.Of(app)` or
  however the module exposes it — follow the existing accessor pattern in
  `httpx/module.go`).
- Do not add any global-registry fallback; the private-registry default is a
  deliberate design decision.

### Tests & docs

- Test: register a custom `prometheus.Counter` via `MetricsRegistry()`, hit the
  metrics handler (`Metrics.Handler()` via httptest), assert the custom metric
  and `go_goroutines` both appear in the exposition output.
- README/docs: one short example under the observability section ("adding your
  own metrics"). The doc-snippet CI will compile it.
- IMPROVEMENTS.md §2.2: tick the registry-accessor and Go/process-collector
  checkboxes (the latter is already implemented in code — verify, then tick).
  Leave the gormx pool-stats and cache-counter items unticked; they are
  follow-ups, not part of this task.

Commit as `feat(httpx): expose the Prometheus registry for custom collectors`.

---

## Task 6 — Deterministic lifecycle ordering (reduced scope) `[P2]`

### Problem (verified)

The DI container is a `sync.Map` (`host/host.go:31`); Init, Setup, Start, and
Close all iterate it via `Range`, whose order is unspecified. Close sorts into
bands (`closeServices`, `host/service.go:385`), but `sort.SliceStable` merely
stabilizes an input order that is itself random, so order **within** a band
still varies run to run. `startWorkers` already works around this by sorting DI
workers by type name (`host/workers.go:93`) — evidence the nondeterminism is a
real nuisance.

### Scope decision (do not expand)

Implement **deterministic registration-order iteration only**. Do NOT build the
`Requires()`/`Provides()` service-descriptor graph from review §1 — it adds a
string-ID identity system parallel to the typed container for a need no module
author has demonstrated. If a dependency-graph API is ever wanted, it can be
layered on top of this change later.

### Implementation

- Add an ordered key log next to the map: `keys []any` + `keysMu sync.Mutex` on
  `App`. Every store path (`ProvideService`, `ProvideKey`) appends the key
  **only if it was not already present** (replacement keeps the original
  position — use the `LoadOrStore`-style check or track presence in a set).
  There is no delete path today; if one is ever added it must remove from the
  log.
- Add an internal `orderedRange(fn func(key, value any) bool)` that snapshots
  the key slice under the mutex, then looks each key up in the `sync.Map`
  (skipping keys whose value is gone). Switch `initServices` (both passes),
  `setupServices`, `startServices`, `closeServices` collection, `fatalErrCh`,
  `beginShutdown`, and the DI-worker collection in `startWorkers` to it.
- The fixpoint loop in `initServices` must still see services registered
  *during* a pass: snapshotting per `orderedRange` call already handles this,
  because each outer `for` iteration re-snapshots — verify with the existing
  "service provides services" test.
- Close order becomes: sort by close-order band (ascending), and **within a
  band, reverse registration order** — last registered closes first, mirroring
  construction. This is the one behavioral choice; document it on the
  `CloseEdge/Default/Backend` constants' comment block.
- `startWorkers` can drop its sort-by-type-name workaround in favor of
  registration order; keep the dedupe-by-type logic. This changes DI-worker
  start order from alphabetical to registration order — acceptable, note it in
  the commit message.
- `RangeServices` keeps its documented "order unspecified" contract (switch it
  to orderedRange anyway, but don't strengthen the public promise — leaves
  room to change internals later).

### Tests

- Register services A, B, C; assert Init/Setup/Start call order is exactly
  A, B, C across many iterations (a recorded-calls fake makes this cheap).
- Assert Close within one band runs C, B, A, and bands still order
  Edge < Default < Backend.
- Re-providing an existing (type, name) does not change its position.
- The dynamic "service provides a service during Init" fixpoint test still
  passes and the late service runs after the batch in which it was added.

Commit as `feat(host): make lifecycle iteration deterministic (registration order)`.

---

## Task 7 — Compatibility policy (light) `[P2, docs]`

The CI half (external-consumer build without `go.work`) ships in Task 1. What
remains is documentation:

- Add a `docs/compatibility.md` (linked from README and CONTRIBUTING) stating:
  supported Go versions (current policy: the version in `go.mod`; decide and
  state whether the last two Go releases are supported — check what
  `go 1.26.2` directives force on consumers and consider lowering the
  directive to the minimum actually required by used language features),
  the lockstep multi-module versioning scheme, what pre-v1 semver means here
  (breaking changes allowed in minor releases, always flagged with `!` and
  CHANGELOG migration notes — which the changelog already practices),
  and the deprecation approach (deprecated APIs keep working for at least one
  minor release with a `// Deprecated:` comment).
- Do not build the multi-preset external test matrix from review §11; the
  single consumer-build job from Task 1 is the guard.

Commit as `docs: add compatibility and release policy`.

---

## Out of scope — reviewed and deliberately rejected (do not implement)

| Review item | Why rejected |
| --- | --- |
| §1 full `Requires()/Provides()` descriptor graph | Second identity system next to the typed container; no demonstrated need. Task 6's registration-order determinism covers the actual pain. |
| §4 composition presets | The 20+ `examples/` modules already serve this; preset packages would reintroduce exactly the cross-module coupling Task 1 removes. |
| §7 `grpcx` module | Legitimate but large; build on user demand. Stays tracked in IMPROVEMENTS.md §2.1. |
| §8 messaging capability interfaces (ack/retry/DLQ) | Only one real broker exists (NATS core); abstracting from a single example gets the abstraction wrong. Revisit when JetStream (IMPROVEMENTS.md §2.12) forces real semantics. Documenting *current* delivery semantics on the existing interfaces is welcome as a small docs commit, but no new interfaces. |
| §9 splitting `core` | Breaking restructure across every module for module-graph weight that Task 1 mostly removes anyway; Go consumers only compile packages they import. Revisit at a major-version boundary, if ever. |
| §10 secret providers, aggregated validation, redaction framework | Scope creep; smaller versions already tracked as IMPROVEMENTS.md §2.9 and §2.11. Task 3 covers the load-bearing part (reader injection). |
| §5 outbox metrics/backoff/replay tooling | Real, but must not gate the double-publish fix; keep in IMPROVEMENTS.md §2.6 as follow-ups. |
