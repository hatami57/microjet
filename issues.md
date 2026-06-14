# MicroJet — Roadmap: features & genericity

Production-microservice features MicroJet is missing, and places where it is too
specific. Ordered by recommended execution. Each top-level item should be its own
commit (Conventional Commits format, e.g. `feat:`, `refactor:`; no AI attribution).

---

## Phase 1 — DONE ✅

All three implemented and committed (see `feat(httpx)`, `fix(host)`,
`refactor(core,aws)`):

- ✅ Request/correlation ID middleware + per-request logger + client propagation.
- ✅ Worker panic recovery (`safeRun`).
- ✅ AWS config moved out of `core` (`core.ConfigViper` + `aws.LoadConfig`).

<details><summary>Original Phase 1 spec</summary>

### 1. Request/correlation ID middleware + per-request logger  `feat`

- New `httpx/middleware` RequestID middleware: read `X-Request-ID` (configurable
  header) or generate a UUID; store it on the gin context and echo it in the
  response header.
- Bind the id to a per-request `*slog.Logger` (`logger.With("request_id", id)`)
  and put that logger in `c.Request.Context()` so handlers/services log with
  correlation automatically. Add a `httpx.LoggerFrom(ctx)` / `httpx.RequestID(c)`
  accessor.
- Update the existing `Logger` middleware to include `request_id`.
- Make `httpx.Client` forward the request id outbound (propagate the header when
  a request id is present in the ctx).
- Tests: id generated when absent, preserved when provided, present in response
  header and in log attrs.

### 2. Worker panic recovery  `fix`

- In `host/workers.go`, wrap each worker invocation (`launch` / `runPeriodic`)
  with `recover()`; log the panic + stack and keep the process alive.
- Decision (M2): a panicking/returning periodic worker keeps ticking; a one-shot
  worker that panics is logged and ends. Document the chosen semantics.
- Test: a worker that panics does not crash the process and is logged.

### 3. Decouple AWS (and provider specifics) from `core.Config`  `refactor` (breaking)

- `core.Config` currently hard-codes `AWSConfig` (S3/SQS/DynamoDB). Move
  provider-specific config out of `core` so the foundational module stays
  vendor-neutral. Keep `App`/`Server`/`Log`/`Database`/`Messaging` + `Extra`.
- Approach: let the `aws` (and optionally `messaging`) modules define and load
  their own config section from the `Extra` map (or a typed sub-load), and have
  `host.WithAWS` read it there. Preserve existing TOML keys for users.
- Update `host/aws.go` accordingly; keep behavior identical from the user's POV.

</details>

---

## Phase 2 — DONE ✅

All three implemented and committed (`feat(httpx)`):

- ✅ Prometheus metrics middleware + `/metrics` (RED metrics on a private
  registry, auto-wired into the server alongside `/health` and `/readyz`).
- ✅ HTTP client retries with exponential backoff + jitter (`WithRetry`,
  idempotent methods by default, honors ctx).
- ✅ Configurable CORS middleware (`CORS` / `DefaultCORSConfig`, opt-in).

---

## Phase 3 — DONE ✅

- ✅ Renamed `postgres` → `gormx` (generic GORM helpers; NoSQL/DynamoDB get their
  own packages).
- ✅ `messaging.Client` now takes `context.Context` and a `Message`/headers map
  (enables cancellation + metadata/trace propagation).
- ✅ Named multi-database config (`[databases.<name>]` + `WithDatabasesFromConfig`).
- ✅ JWT auth middleware (`middleware.JWT`).
- ✅ Per-client rate limiting (`middleware.RateLimit`).
- ✅ Redis + in-memory cache in a new `cache` module.

Not done (was lower-priority, not selected): **DB migration guidance
(golang-migrate/goose)** — left as documentation for the consumer to choose.

---

## Phase 4 — DONE ✅

- ✅ OpenTelemetry distributed tracing: new `otelx` module (OTLP/HTTP exporter,
  W3C propagation, ratio sampling, lifecycle) + `host.WithTracing`. Automatic,
  no-op-until-enabled instrumentation across httpx server/client, gormx, and the
  NATS client; request logs carry `trace_id`.
- ✅ Message consumption ergonomics: `host.WithSubscriber` (lifecycle-bound
  subscribe/drain) + `host.WithQueueGroup`; typed handlers
  `messaging.HandleJSON[T]` / `HandleEnvelope[T]` and `NewJSONMessage`.
- ✅ Request validation: `httpx.Body[T]` returns BadRequest with per-field
  details (json-named) via `httpx.ValidationError` / `UseJSONFieldNames`.

---

## Phase 5 — DONE ✅

- ✅ Opt-in migrations module `gormx/migrate` (goose wrapper; dialect derived
  from the gorm driver; Up/Down/Version; runs from a host Setup handler).
- ✅ Transactional outbox `outbox` module: `Enqueue`/`EnqueueJSON` within a tx,
  `Relay` with at-least-once delivery, `host.WithOutbox` (migrate + periodic
  relay worker).
- ✅ Idempotency-key middleware `middleware.Idempotency` (replays stored response
  for repeated non-safe requests; method+route scoped; cache.Cache-compatible
  store).
- ✅ Circuit breaker for `httpx.Client` (`WithCircuitBreaker`; consecutive
  server-side failures open it; half-open trial).
- ✅ `testx` module: in-memory app builder, throwaway DB, fake broker, HTTP
  request helpers.

---

## Deferred earlier (judgment calls, intentionally not done)

- H2 `Error()` prints inner (stdlib-consistent; output-format choice)
- H3 `Must*` reachable from handlers (needs usage-contract policy)
- M3 `MustGetExtra*` panics (taste / API surface)
- M5 `convertTo` reflection → mapstructure (regression risk)
- M6 `Table.Find` → `(T, bool)` (would drop the error return)
