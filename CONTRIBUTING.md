# Contributing to microjet

Thanks for your interest in improving microjet! This document explains how the
repository is laid out and how to work on it locally.

## Repository layout

microjet is a **multi-module monorepo**. Each top-level directory is its own Go
module with its own `go.mod`:

| Module | Path |
|---|---|
| `core` | `github.com/hatami57/microjet/core` |
| `http` | `github.com/hatami57/microjet/http` |
| `host` | `github.com/hatami57/microjet/host` |
| `postgres` | `github.com/hatami57/microjet/postgres` |
| `messaging` | `github.com/hatami57/microjet/messaging` |
| `aws` | `github.com/hatami57/microjet/aws` |
| `types` | `github.com/hatami57/microjet/types` |
| `utils` | `github.com/hatami57/microjet/utils` |

This keeps the dependency footprint small: importing `utils` does not pull in
gin, the AWS SDK, GORM, or NATS.

## Local development

A committed `go.work` file ties the modules together so cross-module changes are
picked up locally without publishing or `replace` directives.

```bash
# Build / vet / test every module:
make build
make vet
make test

# Or target a single module:
cd core && go test ./...
```

If you add a new module, add it to `go.work` (`go work use ./newmodule`) and to
the `MODULES` list in the `Makefile`.

## Coding guidelines

- Run `gofmt`/`go vet` before committing (`make fmt vet`).
- Public functions and types should have doc comments.
- Library code must not call `os.Exit` or `panic` for recoverable errors —
  return an error and let the caller decide. The `host` package exposes
  `Must*` helpers for the `main()` convenience case.
- Add tests for new behavior.

## Releasing (maintainers)

Because each module is versioned independently, cross-module changes are
released by tagging each affected module with a module-scoped tag:

```bash
git tag core/v0.2.0
git tag http/v0.2.0
git push --tags
```

Then bump the corresponding `require` lines in dependent modules to the new
tagged versions.

## Submitting changes

1. Fork and create a feature branch.
2. Make your change with tests.
3. Ensure `make build vet test` passes.
4. Open a pull request describing the change and its motivation.
