# microjet Go Idiomatic Code Review

## Summary
- Go 1.26.2 workspace; modules: `core`, `host`, `http`, `postgres`, `aws`, `messaging`, `types`, `utils`, `versioninfo`.
- `go vet ./...` clean across all modules; `staticcheck` not installed.
- Overall: well-organized, modern Go (generics, `slog`, `errors.As`/`Unwrap`, `maps`/`slices`). Typed pagination and the fluent error builder are real upgrades. Most issues cluster around concurrency lifecycle, DI/reflection ergonomics, error wrapping (`%v` vs `%w`), and a few zero-value/API consistency wins.

---

## Critical

**C1 — `aws/s3.go:30-55` `S3DownloadFiles` leaks goroutines on early error.**
Every request spawns a goroutine before acquiring `sem`; the consumer returns on the first error, leaving N-1 goroutines running with no caller awareness. `ctx` cancellation isn't checked between launches.
→ Use `golang.org/x/sync/errgroup` with `WithContext` + `SetLimit(maxWorkers)`; one `g.Wait()` at the end gives bounded concurrency, propagated cancellation, and clean shutdown.

**C2 — `host/workers.go:85-96` `runPeriodic` uses `time.After` inside a select loop.**
Allocates a `*time.Timer` per iteration that can't be GC'd until it fires.
→ `t := time.NewTicker(interval); defer t.Stop()` outside the loop, then `select { case <-ctx.Done(): return; case <-t.C: }`.

---

## High

**H1 — `core/errors.go` sentinels (`ErrBadRequest`, `ErrNotFound`, …) don't work with `errors.Is`.**
Each `Err*` is a fresh `*Error`; builder methods return copies. No `Is(target) bool` method, so `errors.Is(err, core.ErrBadRequest)` is false even for errors derived from it. The `IsBadRequestError` helpers paper over this with `Type` checks, but the standard `errors.Is` API surprises consumers.
→ Add `func (e *Error) Is(target error) bool` comparing `Type` (and optionally `Subject`). Or rename sentinels (`DefaultBadRequest`) so they don't *look* like `errors.Is` targets.

**H2 — `core/errors.go:117-132` `(*Error).Error()` doubles inner output when wrapped with `%w`.**
The rendered string includes `inner=...`, but `Unwrap()` also exposes it; `fmt.Errorf("...: %w", coreErr)` shows the inner twice.
→ Stop printing `inner` in `Error()`; rely on `Unwrap()`. Expose a `ChainString(err)` helper if you want a full dump in logs.

**H3 — `host/service.go:53,90` `MustResolveService` panics with `*core.Error` at request time.**
Nothing recovers it, so a missing service crashes a running server.
→ Restrict `Must*` to `init()`/`main()` wiring; ensure handler-time DI uses `ResolveService` which already returns `(T, bool)`. Audit the reference consumer.

**H4 — `host/workers.go:69-80` `sync.Map.Range` gives non-deterministic worker start order; silent double-start.**
Services depending on each other can race; a service registered via `WithWorker` *and* implementing `AsyncWorker` starts twice silently.
→ Maintain an ordered slice alongside the `sync.Map`; document precedence and dedupe by `reflect.Type`.

**H5 — `aws/sqs.go:21-29` `SQSSendMessage` logs "client not configured" and returns `nil`.**
Caller thinks the message was sent.
→ `if a.SQSClient == nil { return fmt.Errorf("sqs client is not configured") }`. Mirror the `s3.go:59` pattern. Also guard the `*output.MessageId` deref in the success log.

**H6 — `host/host.go:144-175` HTTP server start goroutine only signals on error.**
Safe today because `httpErrCh` is buffered 1, but it relies on `Start()` returning `nil` for `http.ErrServerClosed`. Fragile.
→ Either send on clean exit too, or wrap HTTP in the same `sync.WaitGroup` pattern as workers. Or run the whole `Run` under `errgroup`.

**H7 — `http/query.go:25-91` `BindQueryParams` silently swallows parse errors.**
`?id=notauuid` returns the zero value with no error to the handler — exactly the kind of bug that becomes a security report.
→ Return `error`; on first parse failure return `core.ErrBadRequest.WithMessage(...)`. Validate the struct-pointer assumption with an early panic in dev.

**H8 — `core/config.go:144-148` `path` package used for filesystem paths.**
`path` is for slash-separated paths (URLs, imports). Portability bug masquerading as a Linux non-issue.
→ Use `path/filepath`.

---

## Medium

**M1 — `core/errors.go:172-178` `copySliceToMap` drops non-string keys silently.**
This is slog's variadic pattern, but slog logs `!BADKEY` on misuse; here mistakes vanish.
→ At least set `dst["!BADKEY"] = src[i+1]` on bad keys.

**M2 — `host/workers.go:51-62` Async worker semantics ambiguous.**
Doc says async worker "should block until ctx cancelled," but if it returns early the wrapper does nothing. Restart with backoff, or document "returning ends the worker for good."

**M3 — `core/config.go:213-223` `MustGetExtra*` panics with raw strings.**
Startup-only, so lower-severity than H3 — but consider dropping `Must` flavors entirely or documenting "init/main only."

**M4 — `aws/s3.go:81-93` Hand-rolled Read/Write loop with a 20 MB buffer.**
→ Use `io.Copy(f, result.Body)`.

**M5 — `core/config.go:234-272` `convertTo[T]` is 60 lines of reflection + JSON.**
Viper already uses mapstructure; lean on `mapstructure.Decode` with `DecodeHook`s rather than maintaining this.

**M6 — `http/utils.go:104-110` `Body[T]` returns `*T`.**
For request bodies, returning `T` is more ergonomic and matches Go's "value-shaped data" idiom. Same for `Find` → `(TEntity, bool)` mirrors `sync.Map.Load`.

**M7 — `messaging/messaging.go:54-58` `Disconnect()` ignores `Drain()` error.**
At minimum log it. Same pattern in some `defer x.Close()` calls (e.g. `aws/s3.go:69`).

**M8 — `host/host.go:202-257` `Close()` runs shutdown branches concurrently with no per-branch timeout.**
`HTTPServer.Stop` has 10s; `Disconnect`, `db.Close`, `closeServices` don't. A misbehaving service blocks shutdown forever.
→ Per-branch `context.WithTimeout` or a deadline on the outer `wg.Wait()`.

**M9 — `postgres/pagination.go:195-206` `getFieldValue` uses unchecked `field.Interface().(TValue)`.**
Panics if `TValue` doesn't match. Use the two-value form, return a typed error.

**M10 — `http/middleware/tenant.go:67-74` Case-insensitive linear scan of `URL.Query()` for `tenantId`.**
Query keys are case-sensitive per RFC 3986; the policy is fine but undocumented and slow.
→ Canonicalize on input or document the policy.

**M11 — `http/*` directory contains package `httpx`.**
Idiomatic Go: package name matches directory's last element. The reason is the `net/http` conflict, but you can alias at call sites. Either rename the dir to `httpx/` or add a top-of-file note justifying the divergence.

---

## Low

- **L1** — Replace `interface{}` with `any` (`utils/converters.go`, `types/money/money.go`). `gofmt -r 'interface{} -> any' -w .`
- **L2** — `core/time.go:17` — `var Clock SystemClock` but methods are on `*SystemClock`. Either make `Clock = &SystemClock{}` or switch to value receivers.
- **L3** — `aws/s3.go:67,77,85,91` — use `%w` for wrapping, not `%v`.
- **L4** — `core/config.go:154` — `app.debug` defaults to `true`. For a library used in production this is risky (gin mode, Swagger, inner errors in HTTP responses). Default to `false`.
- **L5** — `host/service.go:11-13` — misaligned `var (...)` block; `gofmt -w ./...`.
- **L6** — `core/errors.go:120-127` — `slices.Sorted(maps.Keys(e.Params))` replaces the hand-rolled sort.
- **L7** — `host/host.go:69-76` — `MustNew`/`MustRun` should wrap the panic value with context: `panic(fmt.Errorf("host.MustNew: %w", err))`.
- **L8** — `versioninfo/versioninfo.go:14` — `GoVersion` via ldflags is unnecessary; `runtime.Version()` is authoritative.
- **L9** — `utils/utils.go:17` — `IgnoreError[T any]` is a footgun. Remove or rename to `Must`/`Unsafe`.
- **L10** — `postgres/table.go:65-70` — `Table.DB` is exported, defeating the wrapper.

---

## Nit

- **N1** — `WithMessage` accepts variadic key-values, conflating "set message" with "set params." Split or document.
- **N2** — `host/host.go:147-148, 196-199` — duplicated `signal.Notify` block; extract a helper.
- **N3** — `http/utils.go:113-122` — `PagedRequest` silently falls back to `pageSize=10`. Acceptable but worth a comment.
- **N4** — `postgres/pagination.go:144-145, 190-191` — trailing-space formatting; `gofmt`.
- **N5** — `core/logger.go:111` — concatenates `"Z"` to formatted UTC time; use the literal `Z` in the format string instead.

---

## Keep doing this

1. **Generics used judiciously** — `Table[TEntity]`, `PageRequest[TEntity, TValue]`, `PagedResult[T]`. Typed pagination is a real win, and legacy untyped flows are explicitly documented as such.
2. **`slog` adoption with a custom plain-text handler** that correctly propagates `WithAttrs`/`WithGroup` — most homegrown handlers get group propagation wrong.
3. **Useful doc comments with code examples** on exported symbols (`BaseRepository`, `PageRequest`, `WhereIf`).
4. **Fluent `*Error` builder is immutable** (returns copies; covered by `TestBuilderIsImmutable`) — exactly right given the sentinel-template usage.
5. **`closeOnce sync.Once` + parallel shutdown** in `App.Close` — clean and correct modulo per-branch timeouts (M8).

---

## Recommended order

1. Fix the goroutine/timer leaks (**C1**, **C2**).
2. Add `(*Error).Is(target) bool` so sentinels work with `errors.Is` (**H1**) — small change, big ergonomic payoff.
3. Audit non-init `Must*`/panic sites reachable from handlers (**H3**, **M3**).
4. Stop swallowing errors in `SQSSendMessage` and `BindQueryParams` (**H5**, **H7**).
5. Replace `path` with `path/filepath` (**H8**); `interface{}` → `any` sweep (**L1**).
6. Add `staticcheck` to CI alongside `go vet`.
