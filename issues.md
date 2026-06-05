# microjet — Roadmap: features & genericity

Production-microservice features microjet is missing, and places where it is too
specific. Ordered by recommended execution. Each top-level item should be its own
commit (Conventional Commits format, e.g. `feat:`, `refactor:`; no AI attribution).

---

## Phase 1 — Do now (high value, low controversy)

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

---

## Phase 2 — Do next (opt-in, common)

### 4. Prometheus metrics middleware + `/metrics`  `feat`
- RED metrics: request count, error count, duration histogram, labeled by
  method/route/status. Expose `/metrics`. Register from host like `/readyz`.

### 5. HTTP client retries with backoff  `feat`
- Add retry policy to `httpx.Client` (max attempts, backoff, retry on network
  errors + configurable status codes, idempotent methods by default). Honor ctx.

### 6. CORS middleware  `feat`
- Configurable allowed origins/methods/headers; opt-in via a middleware ctor.

---

## Phase 3 — Decide first (breaking / API direction)

- **Rename `postgres` → `store`/`repo`/`gormx`**: package is generic GORM CRUD +
  cursor pagination that also works with SQLite; the name misleads.
- **Add `context.Context` + message metadata/headers to `messaging.Client`**:
  current `Publish(subject, []byte)` can't carry deadlines, cancellation, trace
  context, or headers; limits tracing and non-NATS backends.
- **Named multi-database config**: code supports `WithNamedDatabase`, but config
  models only a single `[database]` section.
- **Auth/JWT middleware**, **rate limiting**, **Redis-backed cache**,
  **DB migration guidance (golang-migrate/goose)** — opt-in, lower priority.

---

## Deferred earlier (judgment calls, intentionally not done)
- H2 `Error()` prints inner (stdlib-consistent; output-format choice)
- H3 `Must*` reachable from handlers (needs usage-contract policy)
- M3 `MustGetExtra*` panics (taste / API surface)
- M5 `convertTo` reflection → mapstructure (regression risk)
- M6 `Table.Find` → `(T, bool)` (would drop the error return)
