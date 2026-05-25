package host

import (
	"errors"
	"reflect"

	"github.com/hatami57/microjet/core"
)

var (
	ErrServiceNotRegistered = core.NewInternalError("General", "Service is not registered")
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
	return reflect.TypeOf((*T)(nil)).Elem()
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
	var initErr error
	a.container.Range(func(_, item any) bool {
		if svc, ok := item.(ServiceIniter); ok {
			if err := svc.Init(a); err != nil {
				initErr = err
				return false
			}
		}
		return true
	})
	if initErr != nil {
		return initErr
	}
	a.isServiceInitialized = true
	return nil
}

func (a *App) closeServices() error {
	var errs error
	a.container.Range(func(_, item any) bool {
		if svc, ok := item.(ServiceCloser); ok {
			if err := svc.Close(a); err != nil {
				a.Logger.Error("service close error", "error", err)
				errs = errors.Join(errs, err)
			}
		}
		return true
	})
	return errs
}
