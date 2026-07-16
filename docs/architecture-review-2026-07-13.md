# Architecture Review Backlog — 2026-07-13

This document records a source and architecture review of MicroJet as a public
Go library for both monoliths and microservices. It is intended as a working
backlog: each top-level item can be implemented and verified independently.

## Summary

MicroJet has a solid modular direction: `core` provides shared primitives,
`host` owns application lifecycle and composition, and capabilities such as
HTTP, persistence, cache, messaging, tracing, AWS, and outbox are opt-in Go
modules. The main work now is to make module composition deterministic, reduce
consumer dependency footprint, and make the runtime equally natural to embed in
a monolith or run as a standalone service.

## Recommended implementation order

1. Deterministic dependency-aware lifecycle.
2. Remove unnecessary module dependencies and enforce tidy module files.
3. Context-driven runtime API for embedding.
4. Safe multi-replica outbox relay.
5. Extensible runtime and custom metrics.
6. gRPC and stronger messaging-consumer contracts.
7. Presets, configuration improvements, and public compatibility policy.

---

## 1. Deterministic, dependency-aware service lifecycle `[P1]`

### Finding

Services are stored in `host.App`'s `sync.Map`. The init, setup, start, health,
and close phases iterate that map, whose order is unspecified. Close services
are sorted by close-order bands, but services within a band retain the input map
iteration order and therefore are still nondeterministic.

This is manageable for the built-in modules because many dependencies are
resolved only after initialization. It is much harder for third-party module
authors: a service that relies on another service during a lifecycle phase has
no explicit way to declare or validate that relationship.

### Recommendation

Introduce a small dependency declaration mechanism and compute a deterministic
topological order before lifecycle execution. Keep the existing simple module
API usable for services that have no ordering requirements.

One possible shape:

```go
type ServiceDescriptor interface {
    ServiceID() string
    Requires() []string
    Provides() []string
}
```

Alternatively, let `host.Module` register descriptors separately from service
values. Use the resulting graph for Init, Setup, and Start; reverse it for
close within each close-order band. Detect and report missing dependencies and
cycles before partially starting the application.

### Acceptance criteria

- Lifecycle order is deterministic across runs.
- A missing required service gives a readable startup error.
- Dependency cycles give a readable startup error naming the cycle.
- Existing modules retain their current behavior without requiring descriptors.
- Tests cover dependency order, close order, absent dependencies, and cycles.

### Relevant source

- `host/host.go`
- `host/service.go`
- `host/module.go`

---

## 2. Minimize and verify public module dependencies `[P1]`

### Finding

Some satellite `go.mod` files retain unrelated MicroJet modules and their
transitive dependencies. `go mod why` did not show package-level import paths
for selected entries. This makes consumer builds larger and can introduce
unnecessary version constraints and vulnerability exposure.

### Recommendation

Run `go mod tidy` for every released module in an isolated module/workspace
setup, review the resulting diffs, and add CI that rejects an untidy module.
Avoid relying on a development `go.work` file when deciding what a published
module requires.

### Acceptance criteria

- Each released module lists only direct and transitive requirements needed by
  its import graph.
- CI runs `go mod tidy` per released module and fails on a diff.
- A fresh external consumer can `go get` and build each documented package.

### Relevant source

- `go.work`
- `Makefile`
- `httpx/go.mod`
- `messaging/go.mod`
- `cache/go.mod`

---

## 3. Add a context-driven embedding API `[P1]`

### Finding

`App.Run()` owns signal handling and blocks until termination. That is ideal for
a conventional service binary but inconvenient when MicroJet runs inside a
monolith, CLI, test suite, externally supervised process, or another runtime
that already owns cancellation and shutdown policy.

### Recommendation

Expose explicit runtime control in addition to the current convenience method.
For example, provide `Start(ctx)`, `Wait()`, and `Shutdown(ctx)` (or equivalent)
that expose startup and termination errors. Retain `Run()` and `MustRun()` as
thin standalone-binary conveniences built on top of the explicit API.

The app should expose a root context derived from the caller context, so
background workers and services share one cancellation lineage.

### Acceptance criteria

- An application can be started and stopped without process signals.
- Cancellation of the supplied context starts graceful shutdown.
- Shutdown respects a caller-provided deadline.
- `Run()` preserves current SIGINT/SIGTERM behavior.
- Tests can start and stop the app without global signal state.

### Relevant source

- `host/host.go`
- `host/workers.go`

---

## 4. Provide documented monolith and microservice composition presets `[P2]`

### Finding

The fine-grained module model is good, but new users must decide which modules,
lifecycle hooks, and operational defaults are appropriate for their deployment
style.

### Recommendation

Offer presets as documented constructors or starter templates rather than a
single all-inclusive package. Presets should return ordinary modules so users
can inspect, replace, or omit pieces.

Suggested presets:

| Preset | Typical contents |
| --- | --- |
| Monolith | HTTP, database, cache, migrations, development defaults |
| API service | HTTP, tracing, metrics, readiness, database |
| Worker | messaging, outbox, tracing; HTTP optional |
| Internal RPC service | gRPC, tracing, health, client helpers |

### Acceptance criteria

- Each preset has a small compiling example.
- Users can override one component without duplicating the whole preset.
- Presets do not force optional dependencies on consumers that do not use them.

---

## 5. Make the transactional outbox safe across replicas `[P1]`

### Finding

The outbox relay documents that concurrent relays can double-publish. Multiple
replicas are a normal production topology, so this limitation should be treated
as a primary microservice reliability concern.

### Recommendation

For PostgreSQL and MySQL 8+, select batches inside a transaction using `FOR
UPDATE SKIP LOCKED`. Keep an explicitly documented single-instance fallback for
SQLite. Consider a later claim-then-publish design if holding row locks while
publishing becomes unacceptable.

Also add retry backoff/jitter, relay lag and failure metrics, and an operator
workflow for inspecting or replaying quarantined messages.

### Acceptance criteria

- Two concurrent relays do not process the same claimed row on supported SQL
  dialects.
- SQLite behavior is documented and tested.
- Retry timing is bounded and observable.
- Metrics expose pending count, oldest pending age, successes, failures, and
  quarantined messages.
- Operators can inspect and intentionally replay failed messages.

### Relevant source

- `outbox/relay.go`
- `outbox/module.go`
- `IMPROVEMENTS.md`, section 2.6

---

## 6. Make observability extensible `[P1]`

### Finding

HTTP metrics are exposed, but applications cannot easily register custom
collectors into the same registry. Runtime/process metrics and consistent
adapter metrics are also absent.

### Recommendation

Expose the private Prometheus registry through the HTTP metrics service and
register Go/process collectors. Add opt-in instrumentation for database pool
statistics, cache outcomes, outbox relay operation, messaging consumers,
retries, and circuit breakers. Preserve the private registry default; do not
silently use Prometheus global registration.

### Acceptance criteria

- App code can register a custom collector that appears at `/metrics`.
- Runtime and process metrics appear by default when metrics are installed.
- Named database and cache instances are labeled safely with bounded labels.
- Adapter metrics have tests and documentation.

### Relevant source

- `httpx/server.go`
- `httpx/middleware/metrics.go`
- `IMPROVEMENTS.md`, section 2.2

---

## 7. Add gRPC as a first-class optional transport `[P2]`

### Finding

HTTP is comprehensive, but gRPC is a common Go service-to-service transport.
Users otherwise need to rebuild lifecycle, health, tracing, error translation,
and shutdown conventions outside the framework.

### Recommendation

Add a separate `grpcx` module with a managed server and dial helper. It should
mirror the HTTP operational contract: recovery, request/correlation metadata,
OpenTelemetry propagation, structured logs, errorx-to-status conversion, health
checks, graceful shutdown, and optional reflection in debug mode.

### Acceptance criteria

- `grpcx.Module()` participates in host lifecycle and readiness shutdown.
- Unary and stream interceptors handle recovery, tracing, logging, and errors.
- Standard gRPC health service reflects application readiness.
- Client helper propagates correlation and trace context.
- The module is independently importable and tested.

### Relevant source

- `IMPROVEMENTS.md`, section 2.1

---

## 8. Define richer messaging-consumer contracts `[P2]`

### Finding

The broker abstraction supports publish, subscribe, request/reply, headers, and
lifecycle-bound subscriptions. It does not describe acknowledgement, retry,
dead-letter, concurrency, ordering, or delivery guarantees, which are essential
differences among real message brokers.

### Recommendation

Keep the simple core pub/sub interfaces, but add optional capability interfaces
instead of pretending every transport has the same semantics. Publish a
versioned event-envelope convention and compatibility guidance for event
schemas.

### Acceptance criteria

- Delivery/acknowledgement semantics are explicit in API documentation.
- Retry and dead-letter behavior can be configured when supported by a broker.
- Consumer concurrency and ordering behavior are documented.
- Event envelope schema/version compatibility is documented and tested.

### Relevant source

- `messaging/messaging.go`
- `messaging/subscriber.go`

---

## 9. Further separate foundational contracts from optional features `[P2]`

### Finding

`core` contains both fundamental contracts (lifecycle, correlation, clock,
errors) and optional implementation choices such as Viper-backed configuration,
JSON utilities, decimal money, and other helpers. Every consumer of the core
module inherits its dependencies.

### Recommendation

Keep the lowest-level package focused on contracts and lightweight primitives.
Place Viper configuration, money, JSON, and heavier utilities in independently
importable packages or modules. Preserve compatibility through a staged,
documented migration rather than a sudden break.

### Acceptance criteria

- A consumer using lifecycle contracts does not need Viper or decimal-related
  dependencies.
- Optional feature packages remain easy to discover and import.
- Migration guidance and deprecation windows are published.

### Relevant source

- `core/go.mod`
- `core/configx/`
- `core/types/money/`

---

## 10. Improve configuration for embedding and production operations `[P2]`

### Finding

TOML discovery and environment overrides are convenient for standalone services,
but public-library consumers often need to inject configuration from another
system, validate it before startup, and safely diagnose invalid values.

### Recommendation

Add explicit reader/source injection, aggregated schema validation, secret
provider hooks, redacted diagnostic output, and documented precedence. Keep
automatic working-directory discovery as a convenience, not the only startup
path.

### Acceptance criteria

- An embedded application can provide configuration without filesystem lookup.
- Validation reports all relevant errors before services start.
- Secret values are never emitted in diagnostics.
- Precedence among defaults, files, environment, and injected values is tested
  and documented.

### Relevant source

- `core/configx/config.go`
- `core/configx/viper.go`
- `host/host.go`

---

## 11. Publish compatibility and release guarantees `[P2]`

### Finding

The repository uses a sensible lockstep, multi-module release process. Public
users still need a clear promise about supported Go versions, API stability,
deprecation periods, module compatibility, and upgrade paths.

### Recommendation

Publish a compatibility policy and add external-consumer integration tests that
create fresh modules and build each supported composition preset. Document the
supported Go-version range and avoid requiring a newer Go release unless the
library needs its language/runtime features.

### Acceptance criteria

- Compatibility policy is linked from the README and CONTRIBUTING guide.
- CI builds representative external consumer modules without the repository
  `go.work` file.
- Each release includes migration notes for breaking changes.
- Go version support is tested and documented.

### Relevant source

- `Makefile`
- `scripts/release.sh`
- `CHANGELOG.md`

---

## Verification notes from this review

- `host` tests passed locally.
- This repository is a multi-module workspace, so root-level `go test ./...`
  is not the supported test command. `make test` is the intended module-by-module
  entry point.
- Existing `IMPROVEMENTS.md` already tracks gRPC, metrics, and concurrent
  outbox relay work. This review intentionally preserves those directions and
  elevates deterministic lifecycle, module hygiene, embedding, and replica-safe
  outbox behavior as the most important public-library improvements.
