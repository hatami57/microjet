# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims to
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.39.0] - 2026-09-01

### Added

- **`aws.ParameterStore`** — a second `secretx.ReadWriter`, backed by SSM
  Parameter Store, interchangeable with `aws.SecretStore`:

  ```go ignore
  store := mjaws.Of(app).ParameterStore(mjaws.WithParameterNamePrefix("prod/"))
  ref, err := store.Store(ctx, "smtp/"+tenantID.String(), secretx.New(password))
  ```

  Same interface and same contract, chosen on price: Parameter Store's standard
  tier holds a secret for free where Secrets Manager bills per secret per month,
  and what that fee buys — Lambda rotation, resource policies, cross-region
  replication — is unused by an application that stores a credential its own user
  supplied. Reach for `SecretStore` when one of the three is actually in use, and
  for this otherwise.

  Three differences are worth knowing. `Delete` is immediate rather than
  scheduled, so a stable per-tenant name can be deleted and recreated without the
  `WithForceDelete` that `SecretStore` needs — at the cost of the recovery
  window. The standard tier caps a value at 4KB and an account at 10,000
  parameters per region, both lifted by the account's default tier
  configuration rather than by this code. And a `SecureString` is KMS-encrypted,
  so `Resolve` needs `kms:Decrypt` as well as `ssm:GetParameter`;
  `WithParameterKeyID` names a customer managed key instead of `aws/ssm`.

  The reference is the parameter's path, so it begins with `/` where a Secrets
  Manager reference begins with `arn:` — a stored reference still says which
  store issued it. `SSM` joins the `Service` list, though requesting it is
  optional: the client is built on demand, as `SecretsManager`'s is.

### Fixed

- **`tenant.CachedStore` cached an absent tenant inconsistently** — a `Store` may
  report "no such tenant" two ways, and only one of them went through the
  negative-caching policy. `(nil, nil)` — the spelling `middleware.Tenant` turns
  into a 401, and so the one most stores use — fell through the *success* branch
  and was cached as an ordinary positive entry, while `(nil, NotFoundError)` was
  governed by `WithNegativeTTL`. The same answer was held for a different length
  of time depending only on how the store spelled it, and `WithNegativeTTL`
  silently governed the less common half.

  Both spellings are now one answer, held for one TTL, and replayed on a hit
  exactly as the store gave them — so wrapping a `Store` in a `CachedStore` never
  changes what its caller sees. Any other error is still never cached: a timeout
  says nothing about whether a tenant exists.

### Changed

- **`WithNegativeTTL` now defaults to the positive TTL rather than to off**, and
  `WithNegativeTTL(0)` is what switches negative caching off. This restores the
  contract the type documented before 0.38.0 — "both successful and not-found
  lookups are cached for the configured TTL" — and applies it to both spellings
  above.

  0.38.0 briefly made not-found caching opt-in. That was the wrong default in the
  direction that matters: it leaves a stream of requests carrying an unknown
  tenant ID reaching the tenant store once per request, which is a load amplifier
  reachable before the tenant is authenticated. It was also never true for the
  `(nil, nil)` spelling, so for most stores the "off by default" did not describe
  what the code did. Callers who want the fresher behaviour should pass
  `WithNegativeTTL(0)`, or a few seconds to keep most of the protection.

## [0.38.0] - 2026-08-31

### Added

- **Cross-account AWS access (`aws`)** — `AssumeRoleConfig` returns an SDK config
  that authenticates by assuming a role in another account, for services that act
  inside a customer's account rather than their own:

  ```go ignore
  cfg, err := mjaws.Of(app).AssumeRoleConfig(mjaws.AssumeRoleOptions{
      RoleARN:     tenant.RoleARN,
      ExternalID:  tenant.ExternalID,
      Region:      tenant.Region,
      SessionName: "notification-" + tenant.ID.String(),
  })
  tenantAWS, err := mjaws.Of(app).Derive(cfg, mjaws.SES)
  ```

  The returned config is a *copy* of `DefaultConfig` with its credentials and
  region replaced, which is the point of the method: overwriting those fields on
  `DefaultConfig` itself repoints every client the application already built —
  DynamoDB and its own SES included — at the assumed account. The credentials
  provider is wrapped in a credentials cache, so a config built once serves many
  calls from one `AssumeRole` rather than one per request. A `RoleSessionName`
  that STS would reject is reported here instead of on every later call.

  `Derive` builds a second `*AWS` over that config, inheriting the `[aws]`
  settings and the logger while giving clients only to the named services. Use it
  instead of assembling an `AWS` value field by field: a struct literal leaves
  every field the package adds later at its zero value, and the unnamed clients
  nil, in a way no compiler notices.

  `CallerIdentity` reports the ARN this application authenticates as — the
  principal the other account's trust policy has to name. `STS` and
  `SecretsManager` join the `Service` list, though neither has to be requested:
  both clients are built on demand.

- **`secretx` (`core`)** — separates a secret's storage from its use. Application
  data holds a reference, a `Resolver` turns that reference into the value, and
  the value travels in a `Value` that prints as `[REDACTED]` through `fmt`
  (including `%#v`, and as a field of a containing struct), `slog` and
  `encoding/json`. `Reveal` is the only way to the plaintext, which makes every
  place that needs it greppable.

  `Env` resolves `env:NAME` from the process environment. `Static` keeps secrets
  in memory for tests and local runs and reports itself as `Insecure`, so a
  startup `Guard(resolver, app.Config.App.IsProduction())` refuses to run a
  production deployment that still has it wired in. A reference minted by one
  store is rejected by another rather than silently misread.

- **`aws.SecretStore`** — a `secretx.ReadWriter` over AWS Secrets Manager.
  `Store` creates the secret the first time and adds a version after that, so the
  ARN it returns stays stable across rotations and the row holding it is never
  rewritten. `Delete` schedules deletion after the recovery window by default
  (`WithForceDelete` for names that have to be reusable at once), and a reference
  pointing at nothing gives a not-found error, distinguishable from a failure to
  reach the store.

- **`cache.Loader`** — load-through caching with single-flight deduplication:

  ```go ignore
  senders := cache.NewLoader[*tenantSender](cache.NewMemoryCache(app.Clock), 5*time.Minute)
  sender, err := senders.Get(ctx, tenantID.String(), buildSender)
  ```

  Concurrent misses on one key run the loader once and share the result. A worker
  that fans out — a sweeper claiming a batch, a consumer draining a queue — hits a
  cold key from every goroutine at once, and the hand-written get/load/set turns
  one expiry into a burst of identical reads. A failed load is not cached; a
  `nil` pointer result is, which is what makes "known absent" cost one lookup
  instead of one per request. `NewJSONLoader` stores entries as JSON for sharing
  across replicas.

- **`limitx.Keyed` (`core`)** — caps concurrency per key instead of in total:

  ```go ignore
  release, err := limiter.Acquire(ctx, tenantID)
  if err != nil {
      return err
  }
  defer release()
  ```

  A global worker pool is fair only while every unit of work costs about the
  same. Once the work talks to something the key controls — a customer's SMTP
  server, a partner's API — one slow endpoint holds every worker it is given and
  a pool of N is starved by a single key. Idle keys are forgotten, so the number
  of keys seen does not accumulate.

- **`tenant.WithNegativeTTL` (`core`)** — caches "no such tenant" for its own,
  shorter TTL, so requests carrying an unknown tenant ID cost one lookup instead
  of one per request. Only a not-found error is cached: a timeout or a dropped
  connection is never remembered as an absent tenant. It is off by default
  because it trades freshness for that protection.

### Fixed

- **`tenant.CachedStore` documented negative caching it did not do** — the type's
  comment said "not found" lookups were cached for the configured TTL, while
  `FindTenant` returned the error without storing anything. Code trusting the
  documented behaviour was paying a lookup per request for every unknown tenant.
  The comment now matches the default, and `WithNegativeTTL` opts into the
  behaviour it described.

## [0.37.0] - 2026-08-24

### Added

- **Derived attributes for `aws/dynamo`** — `const=` and `format=` now work on
  ordinary, non-key fields, so a type discriminator or a GSI key is declared once on
  the struct instead of being filled in by every caller:

  ```go
  Type   string `dynamo:"const=MESSAGE"           dynamodbav:"Type"`
  GSI1PK string `dynamo:"format=T:{TenantID}#MSG" dynamodbav:"GSI1_PK,omitempty"`
  GSI1SK string `dynamo:"format=M:{ID}"           dynamodbav:"GSI1_SK,omitempty"`
  ```

  Such a field is recomputed from its source fields on every `Put` and `Update`, so
  a derived index entry cannot drift from the data it is built out of. It is stored
  and read back as a normal attribute — never decoded — so its source fields only
  need `encoding.TextMarshaler`, not the unmarshaling half that a pk/sk component
  requires. An `Update` still writes a derived attribute only when the update names
  it. `New` rejects a derived field that is not a string, is not persisted
  (`dynamodbav:"-"`), or references itself — the last rules out `prefix=` and a bare
  `{}` on a non-key field, both of which would re-encode the previous write and grow
  the value every time.

### Changed

- **`aws/dynamo` rejects unknown `dynamo` tag options** — an option the package does
  not recognise is now an error from `New` instead of being silently ignored. A typo
  such as `dynamo:"pk,format:T:{TenantID}"` (a colon instead of `=`) used to drop the
  whole pattern and write a bare key value, which is invisible until the data is
  already wrong. Structs carrying an unrecognised option will now fail at startup;
  the error names the type, the field and the offending option.

## [0.36.0] - 2026-08-23

### Added

- **Query options for `aws/dynamo`** — `QueryPage` and `QueryGSIPage` now take a
  trailing `...QueryOption`. `Descending()` lists newest-first, `WithFilter(cond)`
  adds a `FilterExpression`, and `ConsistentRead()` makes a table query strongly
  consistent (it is rejected on a GSI, which DynamoDB cannot read consistently).
  Options compose, and passing none leaves the request exactly as it was. A filter
  is applied after the page size is counted, so a filtered page can come back short
  or empty and still carry a next-page token — keep following the token until it is
  nil.

- **`Table.Count` and `Table.CountGSI`** — issue the same key condition as their
  `Query` counterpart with `Select: COUNT` and sum the pages, without fetching any
  items. `WithMaxItems(n)` stops at n, so an unread badge can show "99+" without
  scanning an unbounded partition.

- **`Table.UpdateWith(ctx, item, UpdateSpec)`** — the extended form of `Update`:
  `Set` and `Remove` in one `UpdateExpression` (clearing a sparse-index attribute
  is now expressible) plus an optional `Condition`. An attribute named in both
  `Set` and `Remove` is rejected as a bad request instead of by DynamoDB, and a
  spec with nothing to write returns without a round trip. `Update` is now a thin
  wrapper over it, with unchanged behaviour.

- **`dynamo.ErrConditionFailed`** — a failed `ConditionExpression` is reported as a
  matchable business error rather than an internal one, so optimistic-concurrency
  and idempotent-write callers can branch on `errors.Is(err, dynamo.ErrConditionFailed)`
  without matching on message text.

- **Transactions** — `PutTx`, `UpdateTx`, `DeleteTx` and `ConditionCheckTx` build
  `types.TransactWriteItem` values using the same keys, timestamps and marshalling
  as their non-transactional counterparts, and the package-level
  `dynamo.TransactWrite(ctx, client, items...)` commits a mixed batch spanning
  several item types. A batch over DynamoDB's 100-item limit is rejected before the
  request goes out, and a cancelled transaction reports which item index failed and
  why instead of a bare "transaction cancelled".

- **Text-encoded key fields** — a pk/sk field may now be any type implementing
  `encoding.TextMarshaler` and `encoding.TextUnmarshaler` (`ulid.ULID`,
  `netip.Addr`, `time.Time`, user types), alongside the existing `string` and
  `uuid.UUID`. Previously the encode path accepted anything through `fmt.Sprintf`
  while the decode path rejected it, so such a key wrote correctly and failed on
  every read.

### Changed

- **`dynamo.New[T]` validates key field types** — every field a `pk`/`sk` pattern
  references must be decodable, and `New` now returns an error naming the field and
  type when it is not. This turns what used to be a runtime read failure into a
  startup failure. A `const=` key references no field, so its field's type is still
  free.

### Fixed

- **`aws/dynamo` pagination tokens could not be decoded** — the token held a
  JSON-marshalled `map[string]types.AttributeValue`, and `AttributeValue` is an
  interface that `encoding/json` cannot unmarshal into, so passing a returned token
  back into `QueryPage`/`QueryGSIPage` always failed with a `NextPageToken`
  bad-request error. Tokens now store the DynamoDB wire shape (`{"S": …}`) and round
  trip. Tokens issued by earlier versions are not accepted — they never worked.

### Documentation

- The `aws/dynamo` package doc now documents the `format=` tag option — the only
  way to build a composite key from several struct fields — including `{FieldName}`
  references, the bare `{}` self-reference, `prefix=`/`const=` as sugar over it, the
  supported key field types, and the constraint that two adjacent placeholders with
  no literal between them cannot be decoded.

## [0.35.0] - 2026-08-22

### Added

- **SES support in the `aws` module** — `aws.Module(aws.SES)` initializes an SES v2
  client (`aws.Of(app).SESClient`) alongside the S3/SQS/DynamoDB ones, and
  `aws.SESSendEmail(ctx, req)` sends a simple HTML and/or text email, returning the
  provider message ID that later delivery/bounce/complaint events carry. The request
  covers To/CC/BCC, Reply-To, a per-message configuration set and SES message tags;
  the sender falls back to configuration, so a caller normally passes recipients,
  subject and body only. Malformed requests (no recipient, no subject, no body) are
  rejected as bad-request errors before any call goes out.

- **`aws.SESIsPermanentFailure(err)`** — reports whether a failed send is worth
  retrying: a rejected message, an unverified sender domain, a malformed request or
  a suspended account is permanent, while throttling, service errors and transport
  failures are transient. Retry loops (a delivery sweeper, say) use it to decide
  between another attempt and a final failure.

- **`aws.SESFormatAddress(name, email)`** — renders an RFC 5322 address, quoting an
  ASCII display name and MIME-encoding a non-ASCII one. It backs the sender
  formatting and is exported for callers building their own address lists.

- **`[aws.ses]` config section** — `senderEmail` and `senderName` set the default
  From address and display name, `configurationSet` the configuration set applied to
  every send, and `endpointURL` overrides the SES endpoint for a local stack. All are
  optional; a request may override the sender and the configuration set per message.

- **`[aws]` `s3UsePathStyle`** — addresses buckets as `endpoint/bucket/key` instead
  of `bucket.endpoint/key`. Off by default (AWS needs the virtual-host form); turn
  it on for a local S3 reached through `endpointURL`, where the per-bucket hostname
  does not resolve.

### Fixed

- **`[aws]` `endpointURL` now reaches the clients** — the field was read into the
  config and then never used, so an application pointing the module at LocalStack (or
  any AWS-compatible gateway) silently kept talking to AWS; only `dynamoDBEndpointURL`
  worked. It is now applied as the SDK base endpoint, inherited by every client the
  module builds, with the per-service endpoints (`dynamoDBEndpointURL`,
  `[aws.ses]` `endpointURL`) overriding it for their own client. A blank or
  whitespace-only value is ignored rather than turned into an unreachable base URL.

### Changed

- **`aws` module dependency bump** — pulling in `service/sesv2` upgrades the shared
  AWS SDK core (`aws-sdk-go-v2` 1.41.12 → 1.43.7, `smithy-go` 1.27.1 → 1.27.8) and
  its internal endpoint/config packages. No API change in the existing S3, SQS or
  DynamoDB wrappers.

## [0.34.0] - 2026-08-09

### Added

- **`aws.SQSSendMessageTo(ctx, queueURL, message)`** — sends to an explicit queue
  for callers that publish somewhere other than the configured default. It holds
  the send logic; `SQSSendMessage` is now a thin wrapper that targets `[aws]`
  `sqsQueueURL`. The success log line now carries the `queueURL` it published to.

### Changed

- **`aws.SQSSendMessage` fails fast when no default queue is configured** —
  previously an unset `[aws]` `sqsQueueURL` was passed to the SDK as a nil
  `QueueUrl`; it now returns an internal error naming the missing config.

## [0.33.0] - 2026-07-22

### Added

- **Per-component log levels** — the `[http]`, `[grpc]`, and `[database]` config
  sections each accept an optional `logLevel` that raises the floor for that
  component's own logging, independent of the global `[log]` level. Leave it unset
  to follow the global level (unchanged behavior); set it to quiet a noisy
  component while the rest of the app stays verbose. `[database]` `logLevel = "warn"`
  keeps the app at `debug` without logging every SQL query (values: `debug`, `warn`,
  `error`, `silent`); `[http]` `logLevel = "warn"` logs only 4xx/5xx access lines
  (`"error"` only 5xx), gating just the access-log line and never the request-scoped
  logger handlers use; `[grpc]` `logLevel = "error"` logs only failed RPCs. A new
  `logx.WithMinLevel(logger, level)` helper backs the HTTP/gRPC filtering and is
  exported for reuse; `gormx.NewGormLogger(logger, levelOverride)` exposes the GORM
  logger builder so custom drivers honor the same override.

- **HTTP errors are logged server-side** — the `httpx` `Error` middleware now logs
  every error a handler attaches to the request, each on its own line through the
  request-scoped logger, at a level derived from its type (client-caused 4xx types
  at Warn, internal or untyped faults at Error). A typed `*errorx.Error` is logged
  with its structured fields (subject, message, code, inner cause) rather than a
  flattened string. Because the response renders only the last error and scrubs
  inner causes in production, this is the only place the underlying failures — and
  all of them when a handler attaches several — are captured.

### Changed

- **`httpx/middleware.Logger`** now takes a `minLevel string` argument, and
  **`grpcx/interceptors.LoggingUnary` / `LoggingStream`** now take a trailing
  `minLevel string` argument, threading the per-component level through. Callers
  using the managed `httpx`/`grpcx` servers are unaffected; direct callers of these
  constructors must pass `""` to keep prior behavior.

## [0.32.0] - 2026-07-21

### Added

- **gormx timestamps honor the host clock** — the database Service now wires the
  App's injected `core.TimeProvider` (`host.WithClock`, `core.UTC` by default) into
  GORM's `Config.NowFunc`, so auto-managed `CreatedAt`/`UpdatedAt` columns are
  stamped through the same clock as the rest of the app instead of GORM's internal
  `time.Now()`. This applies to both driver-opened connections (`gormx.Module`) and
  injected ones (`gormx.Inject`), and lets a `core.FixedClock` freeze timestamps in
  tests. `Service.SetClock` exposes the wiring for direct use.

- **`gormx.Table.OmitAssociations()`** — shorthand for `Omit(gormx.Associations)`
  that skips auto-saving associations on `Create`/`Save`/`Update`, writing only the
  entity's own columns. Use it when an entity was loaded with associations preloaded
  but only its own row changed: GORM would otherwise re-upsert each association with
  `ON CONFLICT DO NOTHING`, wasted work that never updates an existing associated row.

## [0.31.0] - 2026-07-17

### Added

- **Explicit runtime API: `App.Start(ctx)` / `App.Wait()` / `App.Shutdown(ctx)`** —
  `Run()` owned SIGINT/SIGTERM handling, created its own root context, and blocked
  until termination, so an App could not be embedded in a monolith, CLI, test, or
  supervisor that already owns cancellation. `Start` brings the app fully up
  without blocking, deriving the worker context from the supplied `ctx`; `Wait`
  blocks until the app begins stopping (ctx cancelled, a service loop exits, or
  `Shutdown` is called) and returns the fatal service error; `Shutdown` gracefully
  stops it, its ctx deadline bounding the drain and worker wait on top of
  `WithCloseTimeout`. `Run()` is reimplemented on top of the three and adds only
  signal handling — its logs, error wrapping, signal behavior, and return value are
  unchanged.
- **Inject or set configuration in code** — `host.WithConfigReader` supplies the
  configuration source directly, bypassing TOML file discovery, for embedding an
  App in a process that already owns configuration or for hermetic tests;
  `configx.NewMapReader` is an in-memory `Reader` seeded from a nested map that
  mirrors the file layout (string values decode to typed fields, so `"5s"`
  populates a `time.Duration`). `host.WithConfigValue` / `host.WithConfigValues`
  set individual values by dotted path (e.g. `"app.shutdownDelay"`); programmatic
  values win over config files, environment variables, and defaults, and work with
  the default file reader as well as any reader implementing the new
  `configx.Setter`. Whatever an injected reader returns is authoritative — the
  env-var override shim applies only to the built-in file reader.
- **gRPC support: new `grpcx` module** — a managed `*grpc.Server` with the same
  operational surface as `httpx`. `grpcx.Module()` / `grpcx.Of(app)` install and
  resolve the server; register generated services from a `Setup` hook via
  `Server()`. The default unary+stream interceptor stack is recovery → request-id
  → logging → errorx-to-status (mapping the six errorx categories to gRPC codes),
  with an `otelgrpc` stats handler that is a no-op until `otelx` is installed. A
  `grpc_health_v1` health service is driven by the same readiness checks as httpx
  `/readyz`, reflection is enabled in debug mode, and `grpcx.Dial` installs the
  matching client interceptors (request-id propagation + tracing). New `[grpc]`
  config section; named servers read `[grpc.<name>]`.
- **NATS JetStream: new `messaging/jetstream` module** — a `messaging.Client`
  with durable, at-least-once delivery. It drops into
  `messaging.Module(jetstream.New())` and upgrades the outbox end to end with no
  outbox changes: `Publish` persists to a stream and blocks for the server ack, so
  an event survives a consumer being offline. Durable pull consumers ack on
  handler success, nak (redeliver) on error, and terminate to an optional
  dead-letter subject after `maxDeliver`. New `[messaging.jetstream]` config
  section with declarative, idempotent stream provisioning. Ships no new
  dependencies beyond the `nats.go` client already used by the core NATS driver.
- **Config validation hook** — a `Configurable` that also implements
  `configx.Validator` (`Validate() error`) has `Validate` called immediately after
  `ReadConfig` at startup (through the new `configx.ReadAndValidate`), so invalid
  settings — an empty DSN, an out-of-range port — fail the boot with a wrapped
  error instead of surfacing at first use.
- **`SECURITY.md`** — a private vulnerability-disclosure policy and
  supported-versions statement, surfaced in the repository Security tab.

### Changed

- **Internal errors are now typed `errorx` errors** — every internal `fmt.Errorf`
  across the shipped modules now returns `errorx.NewInternalError(...)` carrying a
  category and structured params, with `WithInner` preserving the wrapped cause.
  This is behavior-compatible: functions still return `error`, and `errors.Is` /
  `errors.As` chains are unchanged (the two `messaging.ErrTimeout` sentinel wraps
  are kept verbatim so timeout detection still matches). One dependency change:
  `gormx/migrate`, previously free of any microjet dependency, now imports
  `core/errorx`.
- **README slimmed** — per-topic depth (pagination, aggregates, atomic updates,
  tenancy, modules) moved into snippet-checked `docs/*.md` guides; the README
  keeps the overview, install, quick start, configuration, and links.

## [0.30.0] - 2026-07-14

### Changed

- **`gormx.Table` write methods now report affected rows (breaking)** — `Delete`, `Update`,
  `Save`, `Upsert` and `CreateMany` returned a bare `error`, discarding the row count the
  database already sends back; `FindInBatches` likewise discarded the number of rows it
  streamed. All six now return `(int64, error)`, matching `UpdateMap`, `UpdateColumn`,
  `UpdateColumns` and `Exec`, which already did. The count is what distinguishes "no row
  matched" from "the write failed": a zero from `Delete` or `Update` with a nil error means
  the row was already gone or a `Where` guard rejected it, which is how an idempotent
  re-delete or a conditional update is detected. `FindInBatches` reports the total rows
  processed across all batches, so a backfill can report its own progress. Migrate callers:
  `if err := t.Delete(ctx, …); err != nil` becomes `if _, err := t.Delete(ctx, …); err != nil`,
  and likewise for the others — discard the count where it isn't needed.
- **`outbox.Relay.PrunePublished` no longer counts before deleting** — it ran a `Count` and
  then a `Delete` with the same condition purely to report how many rows it removed. It now
  takes the count from the delete itself: one round trip instead of two, and no window in
  which the reported number can go stale.

## [0.29.2] - 2026-07-12

### Fixed

- **`gormx.Table` shared one model struct across all calls, racing under
  concurrent writes** — `UpdateMap`, `UpdateColumn` and `UpdateColumns` hand the
  table's model to GORM, which writes the assigned columns (and any auto-updated
  timestamp) back into that struct via reflection. A single `*TEntity` was
  allocated per `Table` and reused, so concurrent writes through a shared
  repository raced on it (`go test -race` flags it, and a stale primary key could
  leak into a later statement's `WHERE`). Each call now resolves the schema from a
  fresh model value; `entityName` no longer dereferences the shared struct either.

## [0.29.1] - 2026-07-12

### Security

- **`quic-go` bumped to v0.59.1** — v0.59.0 is affected by
  [GO-2026-5676](https://pkg.go.dev/vuln/GO-2026-5676), an HTTP/3 QPACK trailer
  expansion memory exhaustion, and `govulncheck` reports the `http3` symbols as
  reachable. It arrives as an indirect dependency of every module that carries
  the HTTP stack, so the `require` is bumped in each. Importers pick up the fix
  by upgrading to this release.

### Fixed

- **README documented three `httpx` helpers that do not exist** —
  `FindUUIDParam`, `FindQuery` and `FindInt64Query`; the package exports the
  `Get*` forms (`GetUUIDParam`, `GetQuery`, `GetInt64Query`). The new `docs` CI
  job now resolves every documented symbol, so this class of drift breaks the
  build.

### Tooling

- `govulncheck` runs per module in CI, on PRs and on a weekly schedule (new CVEs
  are published against unchanged code). `make vuln` mirrors it.
- `golangci-lint` is enforced in CI against a committed root `.golangci.yml`
  (errcheck, govet, ineffassign, misspell, revive, staticcheck, unused).
- A `docs` job compiles the self-contained ` ```go ` blocks in `README.md` and
  `docs/*.md` against the local modules, and resolves every microjet symbol the
  remaining fragments name. `make docs` mirrors it.

## [0.29.0] - 2026-07-12

### Added

- **Portable DB error classification (`gormx`)** — the postgres and sqlite drivers now
  open with gorm's `TranslateError: true`, so driver-specific constraint violations are
  mapped to gorm's portable sentinels. New `gormx` helpers classify them without importing
  gorm or sniffing driver error codes (e.g. pg `SQLSTATE 23505`): `IsDuplicateKey`,
  `IsForeignKeyViolation`, `IsCheckConstraintViolation`, and `IsRecordNotFound`.
- **Outbox durability and observability knobs (`outbox`)** — the enqueuer now captures the
  caller's trace context and correlation id at enqueue time, so events relayed later from a
  background context keep their lineage (matching the live publisher). New options harden the
  relay: `outbox.MaxAttempts(n)` / `WithMaxAttempts` quarantines a poison message once it has
  failed `n` times (recording it in a new `FailedAt` column instead of retrying forever), and
  `outbox.Retention(d)` / `WithRetention` (via `Relay.PrunePublished`) deletes rows published
  more than `d` ago so the table stays bounded. The relay also drains promptly after each
  enqueue instead of only on its interval.

### Changed

- **`outbox` is now gormx-native (breaking)** — enqueuing moved from the package-level
  `outbox.Enqueue`/`outbox.EnqueueJSON(tx *gorm.DB, ...)` to an `*outbox.Enqueuer`
  (`outbox.NewEnqueuer(db)`, or resolve the shared one with `outbox.Of(app)` /
  `outbox.Lookup(app)` when `outbox.Module` is installed). The enqueuer writes through
  `gormx.Table[Message]`, so calls are context-threaded: enqueuing inside a
  `gormx.BaseRepository.RunTx` joins that transaction — no more passing a raw `*gorm.DB`
  around. The relay likewise drains via `gormx.Table` and is built once (per named database)
  by a `relayService.Setup` hook that fails fast if the database or messaging client is
  missing. Migrate callers: replace `outbox.EnqueueJSON(tx, subj, v)` inside a
  `db.Transaction` with `outbox.Of(app).EnqueueJSON(ctx, subj, v)` inside a `RunTx`.

## [0.28.0] - 2026-07-08

### Added

- **Resolve multiple implementors of one interface (`host`)** — `ResolveAllServices[T](app)`
  returns every service registered under type `T`, keyed by the name it was provided with
  (`""` for the default), so implementors registered side by side under one interface type
  can be enumerated and chosen by a runtime criterion. `ResolveServiceBy[T](app, pred)`
  returns the first registered `T` satisfying a predicate. Resolution is exact-type: an
  instance is discoverable through an interface only if registered under that interface type.

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
