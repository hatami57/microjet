package host

import (
	"errors"
	"reflect"
	"slices"
	"sort"

	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/core/configx"
	"github.com/hatami57/microjet/core/errorx"
)

var ErrServiceNotRegistered = errorx.NewInternalError("General", "Service is not registered")

type ServiceIniter interface {
	Init(app *App) error
}

type ServiceSetupper interface {
	Setup(app *App) error
}

type ServiceStarter interface {
	Start(app *App) error
}

type ServiceCloser interface {
	Close(app *App) error
}

// ErrSource is an optional interface for services that run a background loop and
// report its fatal outcome on a channel (e.g. the HTTP server's listener). Run
// merges every started service's channel and treats a delivery like a shutdown
// trigger, so a crashing long-running service brings the app down cleanly.
type ErrSource interface {
	ErrCh() <-chan error
}

// RangeServices calls fn for each registered service until fn returns false.
// Iteration order is unspecified — callers must not depend on it, even though it
// happens to follow registration order today. It lets satellite packages and
// modules inspect the container — e.g. aggregate health checks across services —
// without needing access to its internals.
func (a *App) RangeServices(fn func(service any) bool) {
	a.orderedRange(func(_, v any) bool { return fn(v) })
}

// storeKey stores value under key, recording key in the registration-order log
// the first time it is seen. Re-storing an existing key replaces its value but
// keeps its original position, so the lifecycle order is stable across replaces.
// Every write into the container goes through here so keys and container stay in
// step. There is no delete path today; if one is added it must also remove key
// from a.keys.
func (a *App) storeKey(key, value any) {
	a.keysMu.Lock()
	defer a.keysMu.Unlock()
	if _, loaded := a.container.Load(key); !loaded {
		a.keys = append(a.keys, key)
	}
	a.container.Store(key, value)
}

// orderedRange calls fn for each registered service in registration order until
// fn returns false. It snapshots the key log under the lock, then looks each key
// up in the container, skipping any whose value is gone. Snapshotting per call
// means fn may register further services (as Init hooks do) without disturbing
// the current walk; the caller re-invokes orderedRange to pick them up.
func (a *App) orderedRange(fn func(key, value any) bool) {
	a.keysMu.Lock()
	keys := make([]any, len(a.keys))
	copy(keys, a.keys)
	a.keysMu.Unlock()

	for _, k := range keys {
		v, ok := a.container.Load(k)
		if !ok {
			continue
		}
		if !fn(k, v) {
			return
		}
	}
}

// serviceKey identifies a service in the container by its Go type and an optional
// name. Naming lets several instances of the same type coexist — a public and an
// admin HTTP server, a "sessions" and a "pages" cache, a primary and an
// "analytics" database — each resolved by passing its name. The zero value (empty
// name) is the default instance, so unnamed Provide/Resolve calls are unaffected.
type serviceKey struct {
	typ  reflect.Type
	name string
}

// keyFor builds the container key for type T and an optional name. Only the first
// name is significant; an absent name selects the default ("") instance.
func keyFor[T any](name []string) serviceKey {
	n := ""
	if len(name) > 0 {
		n = name[0]
	}
	return serviceKey{typ: reflect.TypeFor[T](), name: n}
}

// ProvideService registers service in the container under its type T and an
// optional name. Re-providing the same (type, name) replaces the earlier value;
// distinct names register side by side.
func ProvideService[T any](a *App, service T, name ...string) {
	a.storeKey(keyFor[T](name), service)
}

// ResolveService returns the service registered for type T and the optional name,
// reporting whether one was found.
func ResolveService[T any](a *App, name ...string) (T, bool) {
	raw, ok := a.container.Load(keyFor[T](name))
	if !ok {
		var zero T
		return zero, false
	}
	return raw.(T), true
}

// MustResolveService returns the service for type T (and optional name),
// panicking if none is registered.
func MustResolveService[T any](a *App, name ...string) T {
	if svc, ok := ResolveService[T](a, name...); ok {
		return svc
	}
	subject := reflect.TypeFor[T]().String()
	if len(name) > 0 && name[0] != "" {
		subject += " (name " + name[0] + ")"
	}
	panic(ErrServiceNotRegistered.WithSubject(subject))
}

// ResolveAllServices returns every service registered under type T, keyed by the
// name it was provided with ("" for the default, unnamed instance). Use it to
// enumerate all implementors of an interface — registered side by side under one
// interface type via distinct names — and pick one by a runtime criterion:
//
//	host.ProvideService[Notifier](a, email, "email")
//	host.ProvideService[Notifier](a, sms, "sms")
//	for name, n := range host.ResolveAllServices[Notifier](a) { ... }
//
// Only services registered under T itself are returned; an instance registered
// under a concrete type is not discoverable through its interface. The result is
// a fresh map (never nil) and safe to mutate.
func ResolveAllServices[T any](a *App) map[string]T {
	want := reflect.TypeFor[T]()
	out := make(map[string]T)
	a.container.Range(func(k, v any) bool {
		if key, ok := k.(serviceKey); ok && key.typ == want {
			out[key.name] = v.(T)
		}
		return true
	})
	return out
}

// ResolveServiceBy returns the first service registered under type T that
// satisfies pred, reporting whether one matched. Iteration order is unspecified,
// so pred should identify at most one service (or the caller must not depend on
// which match wins). It is a convenience over ResolveAllServices for criteria-
// based selection:
//
//	h, ok := host.ResolveServiceBy[Handler](a, func(h Handler) bool {
//	    return h.CanHandle(contentType)
//	})
func ResolveServiceBy[T any](a *App, pred func(T) bool) (T, bool) {
	for _, svc := range ResolveAllServices[T](a) {
		if pred(svc) {
			return svc, true
		}
	}
	var zero T
	return zero, false
}

// WithProvider runs fn to register services imperatively within the fluent chain,
// deferring any error to Run/MustRun/Err.
func (a *App) WithProvider(fn HandlerFunc) *App {
	if a.err != nil {
		return a
	}
	return a.fail(fn(a))
}

// ProvideKey stores an arbitrary value under a string key — an escape hatch for
// values not identified by Go type. Prefer ProvideService for services.
func (a *App) ProvideKey(key string, value any) *App {
	a.storeKey(key, value)
	return a
}

// ResolveKey returns the value stored under a string key by ProvideKey.
func (a *App) ResolveKey(key string) (any, bool) {
	return a.container.Load(key)
}

func (a *App) InitServices() *App {
	if a.err != nil {
		return a
	}
	if err := a.initServices(); err != nil {
		return a.fail(err)
	}
	return a
}

func (a *App) initServices() error {
	if a.isServiceInitialized {
		return nil
	}
	a.Logger.Debug("initializing services")

	// Services may provide further services from inside their config or Init
	// hooks (the dynamic "service provides services" case; modules register at
	// build time and so are already present). A single sync.Map.Range is not
	// guaranteed to visit entries added mid-iteration, so we drain to a fixpoint:
	// repeat passes until one full pass configures and initializes nothing new.
	// A service is only initialized after it has been configured, so a service
	// added during the init pass waits for the next iteration's config pass.
	configured := make(map[any]bool)
	initialized := make(map[any]bool)
	configCallCount := 0
	initCallCount := 0

	for {
		var passErr error
		progressed := false

		// Load config for services that manage their own configuration.
		a.orderedRange(func(key, item any) bool {
			if configured[key] {
				return true
			}
			configured[key] = true
			progressed = true
			cfg, ok := item.(configx.Configurable)
			if !ok {
				return true
			}
			configCallCount++
			name := reflect.TypeOf(item).String()
			a.Logger.Debug("loading config for service", "type", name)
			if err := cfg.ReadConfig(a.configReader); err != nil {
				a.Logger.Error("failed to load config for service", "type", name, "error", err)
				passErr = err
				return false
			}
			return true
		})
		if passErr != nil {
			return passErr
		}

		// Initialize configured services. host.ServiceIniter (receives *App) takes
		// precedence over core.Initer for services that need host-level DI.
		a.orderedRange(func(key, item any) bool {
			if initialized[key] || !configured[key] {
				return true
			}
			initialized[key] = true
			progressed = true
			name := reflect.TypeOf(item).String()
			a.Logger.Debug("initializing service", "type", name)
			var err error
			if svc, ok := item.(ServiceIniter); ok {
				initCallCount++
				err = svc.Init(a)
			} else if svc, ok := item.(core.Initer); ok {
				initCallCount++
				err = svc.Init()
			} else {
				return true
			}
			if err != nil {
				a.Logger.Error("failed to initialize service", "type", name, "error", err)
				passErr = err
				return false
			}
			a.Logger.Debug("service initialized", "type", name)
			return true
		})
		if passErr != nil {
			return passErr
		}

		if !progressed {
			break
		}
	}

	a.isServiceInitialized = true
	a.Logger.Info("all services initialized", "configs", configCallCount, "inits", initCallCount)
	a.warnUnusedConfigSections()
	return nil
}

// warnUnusedConfigSections logs a warning for each config-file section that no
// component read, which usually signals a typo or a renamed section (e.g. a
// stale [server] after the rename to [http]) that silently has no effect. It is
// best-effort: readers that don't track usage report nothing.
func (a *App) warnUnusedConfigSections() {
	auditor, ok := a.configReader.(interface{ UnusedSections() []string })
	if !ok {
		return
	}
	for _, section := range auditor.UnusedSections() {
		a.Logger.Warn("config section is present but unused; check for a typo or renamed section",
			"section", section)
	}
}

// setupServices runs the Setup phase for services: every service that implements
// ServiceSetupper (receives *App) or core.Setupper performs post-init work that
// depends on other services being connected (migrations, route registration). It
// runs after initServices, so resources are available, and before the queued
// app.Setup handlers, so those handlers observe the services' setup. Like Init
// and Start, services are visited in registration order.
func (a *App) setupServices() error {
	if a.isServiceSetup {
		return nil
	}
	a.Logger.Debug("setting up services")
	var setupErr error
	a.orderedRange(func(_, item any) bool {
		var err error
		if svc, ok := item.(ServiceSetupper); ok {
			err = svc.Setup(a)
		} else if svc, ok := item.(core.Setupper); ok {
			err = svc.Setup()
		} else {
			return true
		}
		if err != nil {
			name := reflect.TypeOf(item).String()
			a.Logger.Error("failed to set up service", "type", name, "error", err)
			setupErr = err
			return false
		}
		return true
	})
	if setupErr != nil {
		return setupErr
	}
	a.isServiceSetup = true
	return nil
}

// startServices runs the Start phase: every service that implements
// ServiceStarter (receives *App) or core.Starter begins active work (e.g. the
// HTTP server starts listening). It runs after initServices and after setup
// handlers, so resources are connected and routes registered before serving.
// Services are started in registration order.
func (a *App) startServices() error {
	if a.isServiceStarted {
		return nil
	}
	a.Logger.Debug("starting services")
	var startErr error
	a.orderedRange(func(_, item any) bool {
		var err error
		if svc, ok := item.(ServiceStarter); ok {
			err = svc.Start(a)
		} else if svc, ok := item.(core.Starter); ok {
			err = svc.Start()
		} else {
			return true
		}
		if err != nil {
			name := reflect.TypeOf(item).String()
			a.Logger.Error("failed to start service", "type", name, "error", err)
			startErr = err
			return false
		}
		return true
	})
	if startErr != nil {
		return startErr
	}
	a.isServiceStarted = true
	return nil
}

// Close ordering bands. Services are closed in ascending order, so inbound
// "edges" stop accepting work and drain before the "backends" they depend on are
// torn down — e.g. an HTTP server finishes serving in-flight requests while its
// database is still open. Services that do not implement CloseOrderer close at
// CloseDefault. The values are plain ints, so a service needing finer control can
// return any value between or beyond these (lower = earlier).
//
// Within a single band, services close in reverse registration order — the last
// one registered closes first — mirroring construction so a service tears down
// before the ones it was built on top of.
const (
	// CloseEdge closes first: inbound traffic sources that must stop accepting and
	// drain, such as HTTP servers and message subscribers.
	CloseEdge = 0
	// CloseDefault is the order for services that do not implement CloseOrderer —
	// ordinary application services.
	CloseDefault = 50
	// CloseBackend closes last: shared resources others depend on during their own
	// shutdown, such as databases, caches, the message broker, and tracing.
	CloseBackend = 100
)

// CloseOrderer lets a service control when Close is called relative to other
// services. Lower values close earlier; use the CloseEdge/Default/Backend
// constants. Services that do not implement it close at CloseDefault.
type CloseOrderer interface {
	CloseOrder() int
}

// closeOrder reports a service's close order, defaulting to CloseDefault.
func closeOrder(svc any) int {
	if co, ok := svc.(CloseOrderer); ok {
		return co.CloseOrder()
	}
	return CloseDefault
}

// closeServices closes every registered service in ascending CloseOrder so edges
// drain before the backends they rely on. Closing is sequential: a band fully
// completes before the next begins, so an HTTP server's in-flight requests are
// done before the database closes. Within a band, services close in reverse
// registration order — last registered, first closed — mirroring construction.
// The whole sequence is still bounded by the shutdown timeout in close(). Errors
// are collected, not fatal.
func (a *App) closeServices() error {
	type entry struct {
		svc   any
		order int
	}
	var entries []entry
	a.orderedRange(func(_, item any) bool {
		entries = append(entries, entry{svc: item, order: closeOrder(item)})
		return true
	})
	// entries is in registration order. Reverse it, then stable-sort by band: the
	// stable sort preserves the now-reversed order within each band, so a band
	// closes last-registered-first while bands still run ascending (edges first).
	slices.Reverse(entries)
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].order < entries[j].order })

	a.Logger.Debug("closing services", "count", len(entries))
	var errs error
	for _, e := range entries {
		name := reflect.TypeOf(e.svc).String()
		var err error
		switch svc := e.svc.(type) {
		case ServiceCloser:
			a.Logger.Debug("closing service", "type", name, "order", e.order)
			err = svc.Close(a)
		case core.Closer:
			a.Logger.Debug("closing service", "type", name, "order", e.order)
			err = svc.Close()
		default:
			continue
		}
		if err != nil {
			a.Logger.Error("failed to close service", "type", name, "error", err)
			errs = errors.Join(errs, err)
		}
	}
	return errs
}
