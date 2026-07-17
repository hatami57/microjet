# Modules

A **Module** bundles a slice of functionality — its services, routes, config, and
workers — behind one `Register` hook, and may install further modules, forming a
tree. This is how you compose a service out of self-contained features (and how a
feature pulls in the features it depends on) without a giant `main()`. See the
[main README](../README.md) for the surrounding app setup.

```go
type Module interface {
    Register(app *host.App) error
}
```

Install modules with the fluent chain; `WithModule` runs the module's `Register`,
and a module's `Register` installs its children the same way:

```go
type EmailModule struct{}

func (EmailModule) Register(app *host.App) error {
    host.ProvideService(app, &EmailSender{}) // a managed service
    return nil
}

type UsersModule struct{}

func (UsersModule) Register(app *host.App) error {
    app.WithModule(EmailModule{}) // child module
    host.ProvideService(app, &UserService{})
    return app.Setup(func(a *host.App) error {
        registerUserRoutes(a)
        return nil
    }).Err()
}
```

```go
import (
    "github.com/hatami57/microjet/host"
    "github.com/hatami57/microjet/httpx"
)

func main() {
    host.MustNew().
        WithModule(httpx.Module()).
        WithModule(UsersModule{}). // brings EmailModule with it
        MustRun()
}
```

Key behaviors:

- **Recursive** — a module's `Register` can install any number of child modules.
- **Deduplicated** — struct modules install once per type, so a shared module
  imported by several parents (the "diamond" case) is registered exactly once.
  Implement `KeyedModule` (`ModuleKey() string`) to install the same type more
  than once with different config; `host.ModuleFunc` wraps a plain function for
  one-off modules and is never deduplicated.
- **Register provides, `Init` wires** — `Register` should only *provide* services
  and *import* child modules, never resolve dependencies (siblings and children
  may register afterwards). Resolve dependencies in each service's `Init(app)` /
  `Start(app)`, which runs after every module has registered, so registration
  order doesn't matter. Provided services join the normal lifecycle (config →
  init → start → close) regardless of how deeply they were nested.

See [`examples/modules`](../examples/modules) for a runnable three-level tree.
