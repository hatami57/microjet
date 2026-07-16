# Compatibility and release policy

This document states what MicroJet promises about Go versions, module versioning,
breaking changes, and deprecations, so you can depend on it with a clear idea of
what a version bump can and cannot change.

## Supported Go versions

The minimum supported Go version is the one in each module's `go.mod` `go`
directive — currently **Go 1.26**. The toolchain enforces this: a Go release
older than the directive cannot build the module, so 1.26 is a hard floor, not a
recommendation.

That floor tracks the language and standard-library features the code actually
uses, not the newest release for its own sake. Today it is 1.26 because the code
uses `new(value)` (a Go 1.26 language feature) and `sync.WaitGroup.Go` (Go 1.25);
it is not pinned higher than those require. There is no `toolchain` directive, so
you choose your own patch release of Go 1.26+ — the framework does not pin one for
you.

The directive moves forward only when the project adopts a feature from a newer
Go release, and such a bump is called out in the CHANGELOG. In practice this means
MicroJet targets the current Go release; it does not commit to supporting older
releases once it has adopted features from a newer one. If you are pinned to an
older Go, pin to the last MicroJet version whose directive your toolchain
satisfies.

## Multi-module versioning

MicroJet is a multi-module monorepo, and every module is released **in lockstep on
a single version**. A release tags all modules at the same version with
module-scoped tags (`core/v0.30.0`, `httpx/v0.30.0`, …), so you can mix modules
freely as long as they share one version. Mixing versions across modules is not a
supported configuration.

`scripts/release.sh` (via `make release-patch` / `make release-minor`) performs the
whole flow — bumping the internal `require` lines, stamping the CHANGELOG, then
tagging every module — so the versions never drift apart. See the "Releasing"
section of [CONTRIBUTING.md](../CONTRIBUTING.md) for the maintainer workflow.

An external-consumer build in CI compiles a throwaway module — outside the
workspace, with no `go.work` — that imports the published modules together, so a
module that under-declares its dependencies fails CI before release. That is the
concrete guarantee behind "the modules build together for an outside consumer."

## Semantic versioning (pre-v1)

MicroJet is pre-1.0 (`0.y.z`), and follows [Semantic Versioning](https://semver.org)
with the pre-v1 convention that **breaking changes are allowed in minor
(`0.y`) releases**, not just major ones. A minor bump can therefore change an API;
a patch (`0.y.z`) never does.

Every breaking change is:

- **flagged in the CHANGELOG** — marked `(breaking)` in its entry, under the
  released `0.y` version, with a migration note describing what to change; and
- **flagged in the commit** — MicroJet uses [Conventional Commits](https://www.conventionalcommits.org),
  so a breaking change carries the `!` marker (e.g. `feat(gormx)!: …`).

Read the CHANGELOG's `(breaking)` entries before taking a minor upgrade; they are
written to be the upgrade guide.

## Deprecations

When an API is superseded rather than removed outright, it is **deprecated first**:
it keeps working for at least one minor release, carrying a `// Deprecated:` doc
comment that names the replacement (which `gopls` and `staticcheck` surface at call
sites). Only after that grace period may a later minor release remove it — as a
breaking change, flagged as above. This gives you a release in which both the old
and new APIs work, so you can migrate without a hard cutover.
