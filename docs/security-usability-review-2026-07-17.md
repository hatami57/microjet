# MicroJet — Security & Usability Review (2026-07-17)

Findings from a focused review of the public API surface with two questions in
mind: *is it safe by default for a public library?* and *is it simple to adopt
without reading the source?* Same workflow as `IMPROVEMENTS.md` / `ROADMAP.md`:
each top-level item is one commit (Conventional Commits, no AI attribution),
ticked off as it lands. Where a finding overlaps an existing
`IMPROVEMENTS.md` item, it is cross-referenced instead of duplicated.

Legend: **[P1]** security defect or unsafe default — fix first ·
**[P2]** hardening / high-value usability · **[P3]** nice to have.

Every item below includes the evidence (file:line as of this review), the fix
specification, and acceptance criteria, so it can be executed independently by
a coding agent without re-deriving context. Line numbers may drift; the
anchors (function names, config keys) will not.

---

## 1. Security — unsafe defaults & defects

### 1.1 [P1] Client IP is spoofable: gin trusted proxies never configured  `fix(httpx)`

**Problem.** `NewServer` (httpx/server.go:131) calls `gin.New()` and never
calls `SetTrustedProxies`. Gin's default trusts **all** proxies, so
`c.ClientIP()` believes any `X-Forwarded-For` header from any peer. Everything
keyed on client IP is attacker-controlled:

- `middleware.RateLimit` default `KeyFunc` (httpx/middleware/rate_limit.go:35)
  — a client bypasses the limiter entirely by rotating a random
  `X-Forwarded-For` per request, and simultaneously inflates the limiter map
  (one entry per spoofed IP, swept only every `IdleTTL`).
- `middleware.Logger` `"ip"` attribute (httpx/middleware/logger.go:43) —
  forged audit trails.

**Fix.**
- Add `TrustedProxies []string` to `ServerConfig` (`[http] trustedProxies`),
  default empty.
- In `NewServer`, always call `router.SetTrustedProxies(cfg.TrustedProxies)`;
  when the slice is empty pass `nil` — gin then ignores forwarding headers and
  uses the socket peer address. Fail `Init` on an invalid CIDR/IP.
- Document in README `[http]` block: behind a load balancer, set
  `trustedProxies = ["10.0.0.0/8"]` (or the LB's CIDR) for `ClientIP()` to
  honor `X-Forwarded-For`.
- Mention the interaction in `RateLimit`'s doc comment: the default key is
  only meaningful once trusted proxies are configured correctly.

**Acceptance.** A request carrying `X-Forwarded-For: 1.2.3.4` from a
non-trusted peer logs and rate-limits under the peer address, not `1.2.3.4`.
Test: httptest with a forged header, assert `c.ClientIP()` == remote addr; a
second test with the proxy CIDR configured asserts the header is honored.

### 1.2 [P1] JWT middleware verifies with an empty HMAC key when misconfigured  `fix(httpx)`

**Problem.** `middleware.JWT` (httpx/middleware/jwt.go:43) builds a keyfunc
from `cfg.Secret` when `cfg.Keyfunc` is nil. If **both** are unset (zero-value
`JWTConfig{}`, or a secret read from an env var that wasn't set), the
middleware happily verifies HS256 tokens signed with the **empty key** — any
attacker can mint valid tokens. This is the classic silent-misconfiguration
hole and it is exactly how a library user will first hit it: a missing
`APP_…_SECRET` in one environment.

Adjacent gaps in the same constructor:

- No expiry requirement: a token without `exp` never expires
  (`jwt.ParseWithClaims` accepts it). golang-jwt v5 has
  `jwt.WithExpirationRequired()`.
- No issuer/audience validation surface — every integrator re-implements
  claim checks by hand or (more often) skips them.
- `Keyfunc` set without `Algorithms` is documented as dangerous
  (jwt.go:28-31) but only in a comment; nothing enforces it.

**Fix.**
- `JWT(cfg)` must **panic** (it's a constructor called at wire-up time, like
  `template.Must`) when `cfg.Keyfunc == nil && len(cfg.Secret) == 0` with a
  message naming the two fields. Panicking at startup on a security
  misconfiguration is this repo's pattern (`MustNew`, `MustRun`).
- Add to `JWTConfig`:
  - `RequireExpiration bool` — when true, append `jwt.WithExpirationRequired()`.
    Default **true** with an explicit `AllowMissingExpiration bool` escape
    hatch is the safer design; if that is judged too breaking, ship
    `RequireExpiration` default-false now and flip it in the next minor,
    noting it in `docs/compatibility.md`.
  - `Issuer string` → `jwt.WithIssuer(...)` when non-empty.
  - `Audience string` → `jwt.WithAudience(...)` when non-empty.
  - `Leeway time.Duration` → `jwt.WithLeeway(...)` when > 0.
- When `Keyfunc != nil && len(Algorithms) == 0`, panic as well — the comment
  already says "always set this"; make it true. (JWKS keyfuncs know their
  algorithms; there is no legitimate "any algorithm" case.)

**Acceptance.** Unit tests: zero-value config panics; token without `exp`
rejected under the default; wrong issuer/audience rejected when configured;
existing HS256 tests still pass. README JWT snippet updated (it is
compile-checked by `scripts/check-doc-snippets.sh`).

**Cross-ref.** JWKS keyfunc helper is IMPROVEMENTS.md §2.10 — do it after
this, on top of the enforced-`Algorithms` rule.

### 1.3 [P1] Postgres DSN built by string interpolation — breaks/injects on special characters  `fix(gormx)`

**Problem.** gormx/postgres/postgres.go:35 builds the keyword/value DSN with
`fmt.Sprintf("host=%s … password=%s …")`. A password (or user, or db name)
containing a space, single quote, or backslash corrupts the DSN — at best a
confusing connection failure, at worst the value's tail is parsed as *other
connection parameters* (a config-injection primitive: a password of
`x sslmode=disable host=evil` rewrites the connection target). Secrets
managers routinely generate passwords with such characters, so this is also a
real adoption blocker, not just a hypothetical.

**Fix.** Quote every value per libpq keyword/value rules: wrap in single
quotes, escaping `\` → `\\` and `'` → `\'`. One small helper
(`func quoteDSNValue(s string) string`) + tests with quote/backslash/space
passwords. Apply to all six fields, not just password.

**Acceptance.** Connection succeeds (integration test against the existing
Postgres test harness — see outbox/relay_postgres_test.go for the pattern)
with a password containing `' \ ` and spaces; unit test asserts the exact DSN
string produced.

### 1.4 [P1] NATS URL with credentials is logged and embedded in errors verbatim  `fix(messaging)`

**Problem.** NATS URLs commonly carry credentials
(`nats://user:pass@host:4222` or `nats://token@host`). `Connect`
(messaging/nats/nats.go:93-98) puts `c.Config.URL` into both the wrapped
error ("failed to connect to NATS at %s") and the `Info` log ("connected to
NATS", "url", …). Both flow into log aggregation.

**Fix.** Redact userinfo before display: parse with `url.Parse`; when
`u.User != nil`, replace with `u.User.Username() + ":xxxxx"` (or drop userinfo
entirely — pick one, apply to both the log line and the error). On parse
failure, log the URL host-part heuristically or `"<unparseable>"` — never the
raw string. Keep the raw URL only for `nats.Connect` itself.

**Acceptance.** Unit test: `Connect` against an unreachable
`nats://alice:hunter2@127.0.0.1:1` returns an error whose string does not
contain `hunter2`; same for the connected-log path (use a slog handler that
captures records).

### 1.5 [P2] Idempotency middleware: cross-client replay, unbounded stored bodies, unbounded key length  `fix(httpx)`

**Problem.** httpx/middleware/idempotency.go:

1. **Cross-principal replay.** The store key is
   `method + route + client-supplied key` (idempotencyKey, line 148) — no
   tenant/user scoping. On any multi-user API, client B who sends the same
   `Idempotency-Key` value as client A (guessed, leaked, or simply a
   collision like `"1"`, `"retry"`, a UUID pasted into docs) receives **A's
   stored response**, including whatever personal data it contains. This is
   an information-disclosure bug for the common case, not an edge case:
   nothing tells users the keyspace is shared.
2. **Unbounded stored response.** `responseCapture` buffers the entire body
   (line 119) and stores it — a 200 MB file download endpoint behind this
   middleware buffers 200 MB per request and writes it to the cache.
3. **Unbounded key.** The header value flows into the cache key uncapped — a
   64 KB header makes a 64 KB cache key.

**Fix.**
- Add `WithIdempotencyScope(fn func(*gin.Context) string)` option; its return
  value is mixed into the store key. Document composing it with the JWT
  middleware (`JWTClaimsFromContext` → `sub`) and/or the tenant middleware
  (`GetTenantID`). In the doc comment, state plainly that **without a scope
  the keyspace is shared across all clients** and the middleware is only safe
  on single-principal or trusted-client APIs.
- Hash the final key: `"idem:" + hex(sha256(method|route|scope|key))` — fixes
  (3) for free, keeps cache keys uniform, and stops raw client input reaching
  Redis key space. (Replaces the current concatenation; the stored-value
  format is unchanged, and keys are cache-internal so no compat concern.)
- Add `WithIdempotencyMaxBodySize(n int64)` (default e.g. 1 MiB). The capture
  writer stops buffering past the cap and marks the response non-storable
  (still streamed to the client untouched); oversized responses are simply
  not replayed.

**Acceptance.** Tests: two different scopes with the same key get different
responses; response larger than the cap is served correctly but not stored;
store keys are fixed-length hashes.

### 1.6 [P2] Request logger writes the full query string — tokens and PII end up in logs  `fix(httpx)`

**Problem.** middleware/logger.go:34 appends `?RawQuery` to the logged path.
Query strings routinely carry `?token=…`, `?code=…` (OAuth callbacks — the
README's own `WriteAutoPostForm`/`MergeParams` target payment-callback flows),
`?tenantId=…`. These land in every log aggregator at Info level.

**Fix.** Redact by default: keep logging the query but replace the values of
a default-sensitive param set (`token`, `access_token`, `id_token`, `code`,
`password`, `secret`, `api_key`, `apikey`, `key`, `signature`) with `[redacted]`,
case-insensitively. Make `Logger` variadic-optional
(`Logger(logger, opts ...LoggerOption)` — backward compatible) with
`WithRedactedParams(names ...string)` to extend/replace the set and
`WithoutQuery()` to drop the query entirely. Redaction operates on the raw
query without re-encoding the rest (split on `&`/`=`, compare unescaped key
names) so log lines remain grep-able.

**Acceptance.** Unit test with a captured slog handler:
`/cb?code=abc&state=x` logs `code=[redacted]&state=x`; custom option redacts
a custom param; `WithoutQuery` logs the bare path. Existing `Logger(logger)`
call sites compile unchanged.

### 1.7 [P2] `/readyz` exposes raw dependency error strings to unauthenticated callers  `fix(httpx)`

**Problem.** `runReadinessChecks` (httpx/server.go:112) returns
`"error: " + err.Error()` per check and `/readyz` serves that JSON to anyone.
Dependency errors leak internals — DB hosts, connection strings fragments,
internal service names — the same class of leak `middleware.Error` carefully
prevents (error.go:60-71: "never expose the raw string in production").

**Fix.** Mirror the Error middleware's debug gate: when `cfg.Debug` is false,
`/readyz` reports each failing check as `"error"` (name + status only); the
full `err.Error()` detail is logged server-side at Warn instead. When debug is
true, keep today's verbose body. Kubernetes only needs the status code.

**Acceptance.** Test: failing check with debug=false → 503, body contains the
check name and `"error"` but not the underlying message; debug=true preserves
current behavior; the detail appears in captured logs.

### 1.8 [P2] Default body size is unlimited — `BodyLimit` exists but nothing wires it  `feat(httpx)`

**Problem.** `middleware.BodyLimit` shipped as opt-in (README "HTTP Hardening
Middleware"), so the default server accepts request bodies of any size —
`httpx.Body[T]` will happily decode a multi-GB JSON post. Opt-in hardening
that nobody discovers is hardening that doesn't exist; the framework's pitch
is "production-grade with minimal boilerplate".

**Fix.** Add `MaxBodyBytes int64` to `ServerConfig` (`[http] maxBodyBytes`).
When > 0, `NewServer` installs `middleware.BodyLimit(cfg.MaxBodyBytes)` in the
default stack (after RequestID/logging, before user routes). Choose the
default: `4 MiB` is a sane JSON-API ceiling; `0` (off) preserves exact
current behavior — prefer **4 MiB default** and call it out in the changelog
as a behavior change with a one-line config opt-out (`maxBodyBytes = 0`).
Routes that need more (uploads) use a route-group without the global limit —
document that pattern, or apply the limit as skippable via
`c.Set("skipBodyLimit", true)` if group-level bypass proves awkward.

**Acceptance.** Default server rejects a body over the limit with 413;
`maxBodyBytes = 0` accepts it; README `[http]` config block documents the key
(snippet-checked).

### 1.9 [P2] Rate limiter state can be ballooned by unauthenticated traffic  `fix(httpx)`

**Problem.** limiterStore (httpx/middleware/rate_limit.go:76-99) creates one
entry per distinct key with no cap; the sweep runs at most once per `IdleTTL`
(default 10m). With spoofed XFF (§1.1) or a rotating botnet, that is
unbounded memory growth for 10 minutes at a time.

**Fix.** After §1.1 lands (which removes the trivial spoof), add a `MaxKeys
int` config (default e.g. 100_000). On insert past the cap, evict the
oldest-`lastSeen` entry (a linear scan at cap is fine — it happens only under
attack; or keep an LRU list if preferred). Never fail open per-request: when
at cap, still create the limiter by evicting.

**Acceptance.** Test: insert `MaxKeys+n` distinct keys, map length stays ≤
`MaxKeys`, most-recently-seen keys survive.

### 1.10 [P3] CORS wildcard + credentials silently reflects any origin  `fix(httpx)`

**Problem.** `resolveCORSOrigin` (httpx/middleware/cors.go:109-115) with
`AllowOrigins: ["*"]` **and** `AllowCredentials: true` reflects the request
origin — effectively "any site on the internet may make credentialed requests
and read the responses", which disables the browser's cross-origin protection
for cookie-authenticated APIs. It is documented (cors.go:14-16), but a
security-critical combination should be loud, not a comment.

**Fix.** In `CORS(cfg)`, when both are set, log a Warn once at construction
("CORS: wildcard origin with credentials reflects every origin; list explicit
origins") — constructor takes no logger today, so use `slog.Default()`, or
(preferred, still small) panic and require the user to pass
`AllowOrigins: []string{"*"}` **plus** a new explicit
`DangerouslyReflectAnyOriginWithCredentials bool` — decide by taste; the
warn-once is the non-breaking floor.

**Acceptance.** Constructing that config emits the warning exactly once;
behavior otherwise unchanged.

### 1.11 [P3] Warn when `debug = true` in a production environment  `feat(httpx)`

**Problem.** `[http] debug = true` turns on Swagger, pprof, and raw inner
errors in responses (server.go:170-173, error.go:29). The config also carries
`[app] environment`. Nothing stops the classic "debug left on in prod".

**Fix.** In `httpx.Module` init (or `NewServer` if the app config is
reachable there), when server debug is true and the app environment equals
`"production"`, log one Warn: "http debug mode is enabled in a production
environment: /debug/pprof, /swagger and error internals are exposed". Do not
refuse to boot — staging setups legitimately do this.

**Acceptance.** Unit test over the module init with `environment=production`,
`debug=true` captures the warning; no warning otherwise.

### 1.12 [P3] `/metrics` is unauthenticated on the service port  `docs`

**Problem.** `GET /metrics` (server.go:152) is served on the main listener
with no auth. Metrics leak route names, request volumes, Go runtime details.
This is a standard trade-off (most shops scrape the service port inside the
cluster) — the gap is that it is undocumented.

**Fix.** Documentation only, for now: a short "Exposure" note in the README
Metrics section — /metrics (and /readyz, /health) are unauthenticated;
restrict them at the network layer (NetworkPolicy / ingress rules), or front
them with `middleware.JWT`/an IP allowlist on a route group if the port is
internet-facing. A separate management-port listener is deliberately out of
scope until someone asks (record as a design decision, not a TODO).

---

## 2. Secrets & credentials hygiene

### 2.1 [P2] Secrets-from-files (`*_FILE`) — already specified  `feat(core)`

IMPROVEMENTS.md **§2.9** already specifies this (read
`APP_DATABASE_PASSWORD_FILE=/run/secrets/db_password` in `applyEnvOverrides`).
This review re-confirms it as the single highest-value secrets improvement:
today every secret (DB password gormx/config.go:9, Redis password
cache/cache.go:33, AWS keys aws/config.go:7-8, JWT secret via user config)
must transit either a TOML file on disk or a plain env var. Execute §2.9 as
specified; no changes to its design needed. Note for the implementor: the
shim lives in core/configx (see the `applyEnvOverrides` doc comment,
viper.go:80-100), and per the memory note it must stay — viper's
`Unmarshal` ignores `AutomaticEnv`.

### 2.2 [P2] SECURITY.md — already specified  `docs`

IMPROVEMENTS.md **§4.5**. Unchanged; just do it. (Contact:
software.apan@gmail.com or GitHub private vulnerability reporting; latest
minor supported; response expectation.)

### 2.3 [P3] Prefer the AWS default credential chain in docs  `docs`

aws/aws.go:58-62 already falls back to the SDK default chain when
`accessKey`/`secretKey` are unset — good. But the README `[aws]` example
(`accessKey = "your-access-key"`) teaches static keys in a config file as the
happy path. Flip the docs: default-chain (IRSA / instance profile / SSO /
env) is the recommended path; static keys in `[aws]` are the fallback for
local dev against localstack. One README edit + a comment on `aws.Config`.

---

## 3. Usability as a public library

### 3.1 [P1] Database pool settings are hardcoded  `feat(gormx)`

**Problem.** gormx/postgres/postgres.go:55-57 pins `MaxIdleConns(5)`,
`MaxOpenConns(10)`, `ConnMaxLifetime(1h)` for every user. 10 open conns is
far too small for a busy service and unchangeable without abandoning the
driver — the first wall any real adopter hits.

**Fix.** Add to `gormx.Config` (read from `[database]` / `[database.<name>]`):
`maxOpenConns int` (default 10), `maxIdleConns int` (default 5),
`connMaxLifetime time.Duration` (default "1h"), `connMaxIdleTime
time.Duration` (default 0 = unset). Defaults preserve today's behavior. Apply
in the postgres driver; sqlite driver ignores pool size but should accept the
fields silently. Register defaults via `SetDefault` in the module's
config-read (matching httpx/server.go:207-213 style) so env overrides
(`APP_DATABASE_MAXOPENCONNS=50`) work. Document in the README `[database]`
block.

**Acceptance.** Integration test (or unit on the extracted pool-config
helper): configured values reach `sql.DB` (`Stats().MaxOpenConnections`);
defaults unchanged when unset.

### 3.2 [P2] `sslMode` has no default — verify and default it  `fix(gormx)`

**Problem.** `gormx.Config.SSLMode` (gormx/config.go:11) has no `SetDefault`,
so an unset value produces `sslmode=` (empty) in the DSN (postgres.go:35).
Depending on pgx's parser this is either an error or an accidental default —
both bad: one is a confusing boot failure, the other an implicit security
posture. The README example teaches `sslMode = "disable"`, which then gets
copy-pasted to production.

**Fix.** First verify empty-value behavior against pgx (one throwaway test).
Then: `SetDefault("database.sslMode", "prefer")` — encrypted when the server
supports it, still works against dev containers. Change the README example to
`"prefer"` with a comment ("use \"verify-full\" in production, \"disable\"
only for local containers"). With §1.3's quoting, the empty-string case also
becomes well-formed either way.

**Acceptance.** Booting with no `sslMode` set connects with `prefer`
semantics; DSN unit test asserts the default.

### 3.3 [P2] Redis TLS cannot be enabled through config  `feat(cache)`

**Problem.** `cache.Config` (cache/cache.go:31-35) exposes
addr/password/db/prefix only; `NewRedis` (cache/redis.go:33-37) never sets
`TLSConfig`. Managed Redis (ElastiCache in-transit encryption, Upstash,
Redis Cloud) requires TLS, so today those users must bypass the module via
`NewRedisWithClient` and hand-wire lifecycle — the escape hatch as the only
hatch.

**Fix.** Add `TLS bool` (`[cache] tls`) to both `cache.Config` and
`RedisOptions`; when true set `&tls.Config{MinVersion: tls.VersionTLS12}`
(ServerName is derived by go-redis from Addr). That covers every managed
provider; custom CA/mTLS stays on `NewRedisWithClient`. Optionally also
accept `addr` values in URL form (`redis://`/`rediss://`) via
`redis.ParseURL` when the string contains `://` — one `if`, and it makes the
config copy-pasteable from provider dashboards. Document both.

**Acceptance.** Unit test: options-building function returns a client whose
`Options().TLSConfig` is non-nil when `tls = true`; `rediss://` URL parses to
TLS + password + db.

### 3.4 [P2] NATS authentication has no config surface  `feat(messaging)`

**Problem.** `[messaging]` carries only `url`, `source`, `version`
(messaging/nats/config.go:6-8). Credentials therefore go **inside the URL**
(`nats://user:pass@…`) — which is what makes §1.4 a leak, and which cannot be
cleanly overridden per-environment (`APP_MESSAGING_URL` must re-state the
whole URL, secret included). `New(opts ...nats.Option)` (nats.go:55) exists
but is code-level, and the module wiring may not pass options through — check
`messaging.Module`'s signature and thread them if not.

**Fix.** Add to the nats config: `user`, `password`, `token`, `credsFile`
(the standard `.creds` file for NKey/JWT auth — this is also the
K8s-mounted-secret path, complementing §2.1). Map to `nats.UserInfo`,
`nats.Token`, `nats.UserCredentials` in `Connect`. Precedence: explicit
config fields override URL userinfo; `credsFile` wins over user/password.
Ensure `messaging.Module` / the nats module constructor accepts
`...nats.Option` passthrough for everything else (TLS certs etc.).

**Acceptance.** Unit test on the option-assembly function (no live broker):
each config field produces the corresponding `nats.Option`; README
`[messaging]` block documents the fields.

### 3.5 [P2] Config validation hook — already specified  `feat(core)`

IMPROVEMENTS.md **§2.11** (call `Validate() error` after `ReadConfig` when
implemented). Re-affirmed here because three of the fixes above (empty JWT
secret §1.2, empty DSN fields, missing NATS URL) are instances of the class
it catches. Cheap; do it in the same wave as this document's config work.

### 3.6 [P3] Unused-section detection exists — surface it at startup  `feat(host)`

`UnusedSections` (core/configx/viper.go:143) catches typo'd/renamed config
tables, but verify it is actually invoked and logged by the host after all
modules have configured (grep `UnusedSections` in host/). If it is not wired,
log one Warn listing the unused sections after init — "config sections
[serverr] were not read by any module" is a five-second fix for the user
instead of a silent no-op. If already wired, close this item as done.

---

## 4. Suggested execution order

Small, independent commits; each ticks its box here.

1. **§1.1 trusted proxies** + **§1.9 limiter cap** — one `fix(httpx)` wave
   (same files, same tests).
2. **§1.2 JWT hardening** — `fix(httpx)`; coordinate the
   `RequireExpiration` default decision with docs/compatibility.md.
3. **§1.3 DSN quoting** + **§3.2 sslMode default** + **§3.1 pool config** —
   one `gormx` wave.
4. **§1.4 NATS redaction** + **§3.4 NATS auth config** — one `messaging` wave.
5. **§1.5 idempotency scope/caps**, **§1.6 logger redaction**, **§1.7 readyz
   detail gate**, **§1.8 default body limit** — httpx hardening wave two.
6. **§3.3 cache TLS**, **§2.3 AWS docs flip**, **§1.10/§1.11/§1.12** small
   items, **§3.6** if unwired.
7. Cross-referenced IMPROVEMENTS.md items in their existing order: §2.9
   (secrets files), §4.5 (SECURITY.md), §2.11 (validation hook), §2.10 (JWKS).

Notes for the executing agent:

- Multi-module repo: run `make test` / `make lint` (module auto-discovery)
  and the doc-snippet check (`make docs`) after each wave — several fixes
  touch README config blocks that are compile-/symbol-checked.
- Behavior-changing defaults (§1.2 expiration, §1.8 body limit, §3.2
  sslMode) must be listed in CHANGELOG.md and, where relevant,
  docs/compatibility.md.
- Keep the existing conventions: options as `With*` funcs, `Must*` panics at
  wire-up, config defaults via `SetDefault` inside `ReadConfig`, opt-in
  middleware documented in the README hardening section.
