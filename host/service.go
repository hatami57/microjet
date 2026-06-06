package host

import (
	"errors"
	"reflect"

	"github.com/hatami57/microjet/core"
)

var (
	ErrServiceNotRegistered   = core.NewInternalError("General", "Service is not registered")
	ErrDatabaseNotInitialized = core.NewInternalError("Database", "Database is not initialized")
)

type ServiceIniter interface {
	Init(app *App) error
}

type ServiceCloser interface {
	Close(app *App) error
}

type providedItem[T, V any] struct {
	Key   T
	Value V
}

func ResolveType[T any]() reflect.Type {
	return reflect.TypeFor[T]()
}

func ProvideType[T any](service T) *providedItem[reflect.Type, any] {
	return &providedItem[reflect.Type, any]{Key: ResolveType[T](), Value: service}
}

func ProvideService[T any](a *App, service T) {
	a.container.Store(ResolveType[T](), service)
}

func ResolveService[T any](a *App) (T, bool) {
	raw, ok := a.container.Load(ResolveType[T]())
	if !ok {
		var zero T
		return zero, false
	}
	return raw.(T), true
}

func MustResolveService[T any](a *App) T {
	if svc, ok := ResolveService[T](a); ok {
		return svc
	}
	panic(ErrServiceNotRegistered.WithSubject(ResolveType[T]().String()))
}

func (a *App) ProvideService(item *providedItem[reflect.Type, any]) *App {
	a.container.Store(item.Key, item.Value)
	return a
}

func (a *App) ProvideServices(items ...*providedItem[reflect.Type, any]) *App {
	for _, item := range items {
		a.container.Store(item.Key, item.Value)
	}
	return a
}

func (a *App) ProvideKey(key string, value any) *App {
	a.container.Store(key, value)
	return a
}

func (a *App) ResolveKey(key string) (any, bool) {
	return a.container.Load(key)
}

func (a *App) ResolveService(key reflect.Type) (any, bool) {
	raw, ok := a.container.Load(key)
	if !ok {
		zero := reflect.New(key).Elem().Interface()
		return zero, false
	}
	return raw, true
}

func (a *App) MustResolveService(key reflect.Type) any {
	if svc, ok := a.ResolveService(key); ok {
		return svc
	}
	panic(ErrServiceNotRegistered.WithParams("name", key.String()))
}

func (a *App) WithProvider(provider HandlerFunc) *App {
	if a.err != nil {
		return a
	}
	if provider != nil {
		if err := provider(a); err != nil {
			return a.fail(err)
		}
	}
	if err := a.initServices(); err != nil {
		return a.fail(err)
	}
	return a
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
	var initErr error

	// Load config for services that manage their own configuration.
	a.container.Range(func(_, item any) bool {
		cfg, ok := item.(core.Configurable)
		if !ok {
			return true
		}
		name := reflect.TypeOf(item).String()
		a.Logger.Debug("loading config for service", "type", name)
		if err := core.LoadAll(a.envPrefix, cfg); err != nil {
			a.Logger.Error("failed to load config for service", "type", name, "error", err)
			initErr = err
			return false
		}
		return true
	})
	if initErr != nil {
		return initErr
	}

	// Initialize services. host.ServiceIniter (receives *App) takes precedence
	// over core.Initer for services that need host-level DI.
	a.container.Range(func(_, item any) bool {
		name := reflect.TypeOf(item).String()
		a.Logger.Debug("initializing service", "type", name)
		var err error
		if svc, ok := item.(ServiceIniter); ok {
			err = svc.Init(a)
		} else if svc, ok := item.(core.Initer); ok {
			err = svc.Init()
		} else {
			return true
		}
		if err != nil {
			a.Logger.Error("failed to initialize service", "type", name, "error", err)
			initErr = err
			return false
		}
		a.Logger.Debug("service initialized", "type", name)
		return true
	})
	if initErr != nil {
		return initErr
	}
	a.isServiceInitialized = true
	a.Logger.Info("all services initialized")
	return nil
}

func (a *App) closeServices() error {
	var errs error
	a.Logger.Debug("closing services")
	a.container.Range(func(_, item any) bool {
		name := reflect.TypeOf(item).String()
		a.Logger.Debug("closing service", "type", name)
		var err error
		if svc, ok := item.(ServiceCloser); ok {
			err = svc.Close(a)
		} else if svc, ok := item.(core.Closer); ok {
			err = svc.Close()
		} else {
			return true
		}
		if err != nil {
			a.Logger.Error("failed to close service", "type", name, "error", err)
			errs = errors.Join(errs, err)
		}
		return true
	})
	return errs
}
