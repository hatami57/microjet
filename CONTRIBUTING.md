# Contributing to MicroJet

Thanks for your interest in improving MicroJet! This document explains how the
repository is laid out and how to work on it locally.

## Repository layout

MicroJet is a **multi-module monorepo**. Each top-level directory is its own Go
module with its own `go.mod`:

| Module | Path |
|---|---|
| `core` | `github.com/hatami57/microjet/core` |
| `httpx` | `github.com/hatami57/microjet/httpx` |
| `host` | `github.com/hatami57/microjet/host` |
| `gormx` | `github.com/hatami57/microjet/gormx` |
| `messaging` | `github.com/hatami57/microjet/messaging` |
| `aws` | `github.com/hatami57/microjet/aws` |

The `core` module also ships the `jsonx`, `utils`, `types`, `tenant`, and
`version` subpackages (light, ubiquitous dependencies kept together). This
keeps the dependency footprint small: importing `core` does not pull in gin, the
AWS SDK, GORM, or NATS.

## Local development

A committed `go.work` file ties the modules together so cross-module changes are
picked up locally without publishing or `replace` directives.

```bash
# Build / vet / test every module:
make build
make vet
make test

# The same gates CI enforces:
make lint         # golangci-lint, using the root .golangci.yml
make staticcheck
make vuln         # govulncheck: known CVEs reachable from our code
make docs         # doc snippets still match the real API

# Or target a single module:
cd core && go test ./...
```

`make lint`, `make staticcheck` and `make vuln` need their tools on `PATH`; each
target prints the `go install` line if the binary is missing.

If you add a new module, add it to `go.work` (`go work use ./newmodule`). The
`Makefile` and CI both discover modules by finding `go.mod` files, so there is
no list to update.

## Coding guidelines

- Run `gofmt`/`go vet` before committing (`make fmt vet`).
- Public functions and types should have doc comments.
- ` ```go ` blocks in `README.md` and `docs/` are checked by `make docs`: blocks
  starting with `package` are compiled, and every `pkg.Symbol` reference into a
  microjet package must resolve. Mark pseudo-code ` ```go ignore ` to skip it.
- Library code must not call `os.Exit` or `panic` for recoverable errors —
  return an error and let the caller decide. The `host` package exposes
  `Must*` helpers for the `main()` convenience case.
- Add tests for new behavior.

## Releasing (maintainers)

Every module is released in lockstep on one version, tagged with module-scoped
tags (`core/v0.2.0`, `httpx/v0.2.0`, …). `scripts/release.sh` does the whole
flow — bump internal `require` lines, stamp the CHANGELOG, then pause for
confirmation before committing, pushing and tagging:

```bash
make release-patch   # or: make release-minor
```

Note the ordering constraint it encodes: internal requires are bumped by string
replacement rather than `go get`, because `go get` cannot resolve the next
version until its tags exist.

Describe user-visible changes under a `## [Unreleased]` heading in
`CHANGELOG.md`; the script renames it to the released version.

## Submitting changes

1. Fork and create a feature branch.
2. Make your change with tests.
3. Ensure `make build vet test lint` passes (and `make docs` if you touched
   `README.md` or `docs/`).
4. Open a pull request describing the change and its motivation.
