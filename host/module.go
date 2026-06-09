package host

import (
	"fmt"
	"reflect"
)

// Module is a composable unit of functionality. Register installs the module's
// services, routes, workers, config, and any child modules into the App.
// Modules compose: a module's Register may install further modules via
// app.WithModule, forming a tree. Composition happens at build time — by the
// time services are initialized, everything a module declared is in the
// container.
//
// Convention: Register only *provides* (ProvideService) and *imports* child
// modules; it must not resolve dependencies, because sibling and child modules
// may register afterwards. Cross-service wiring belongs in each service's
// Init(app)/Start(app), which runs in a later phase once every module has
// registered.
type Module interface {
	Register(app *App) error
}

// ModuleFunc adapts a plain function into a Module for small, inline modules
// that do not need their own type.
type ModuleFunc func(app *App) error

// Register implements Module.
func (f ModuleFunc) Register(app *App) error { return f(app) }

// NamedModule is an optional interface; when a module implements it, the returned
// name is used in logs and error messages instead of the Go type name.
type NamedModule interface {
	ModuleName() string
}

// KeyedModule is an optional interface for modules that may be installed more
// than once with different configuration. The returned key identifies the
// instance for deduplication; two instances with distinct keys both install.
// Modules that do not implement it are deduplicated by their Go type.
type KeyedModule interface {
	ModuleKey() string
}

// WithModule installs a module, running its Register hook. Struct modules are
// deduplicated so a shared module imported by several parents (the "diamond"
// case) installs exactly once; the dedup key is the module's ModuleKey if it
// implements KeyedModule, otherwise its Go type. ModuleFunc values are anonymous
// and never deduplicated. Errors are deferred to Run/MustRun/Err.
func (a *App) WithModule(m Module) *App {
	if a.err != nil || m == nil {
		return a
	}
	if key := moduleKey(m); key != "" {
		if _, loaded := a.modules.LoadOrStore(key, struct{}{}); loaded {
			return a
		}
	}
	name := moduleName(m)
	a.Logger.Debug("registering module", "module", name)
	if err := m.Register(a); err != nil {
		return a.fail(fmt.Errorf("registering module %q: %w", name, err))
	}
	return a
}

// WithModules installs several modules in order, short-circuiting on the first
// error like the rest of the fluent chain.
func (a *App) WithModules(mods ...Module) *App {
	for _, m := range mods {
		a.WithModule(m)
	}
	return a
}

// moduleKey returns the deduplication key for a module, or "" if the module must
// not be deduplicated (anonymous ModuleFunc values, which all share one type).
func moduleKey(m Module) string {
	if k, ok := m.(KeyedModule); ok {
		return k.ModuleKey()
	}
	if _, ok := m.(ModuleFunc); ok {
		return ""
	}
	return reflect.TypeOf(m).String()
}

// moduleName returns a human-readable name for logs and errors.
func moduleName(m Module) string {
	if n, ok := m.(NamedModule); ok {
		return n.ModuleName()
	}
	if _, ok := m.(ModuleFunc); ok {
		return "func"
	}
	return reflect.TypeOf(m).String()
}
