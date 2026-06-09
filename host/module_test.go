package host

import (
	"errors"
	"testing"
)

// recordingModule appends its name to *order when registered, so tests can
// assert install order and count.
type recordingModule struct {
	name     string
	order    *[]string
	children []Module
}

func (m recordingModule) Register(app *App) error {
	*m.order = append(*m.order, m.name)
	for _, c := range m.children {
		app.WithModule(c)
	}
	return nil
}

// ModuleKey distinguishes instances of the shared recordingModule type by name,
// so a recursion test can use one type for all nodes.
func (m recordingModule) ModuleKey() string { return m.name }

func newApp(t *testing.T) *App {
	t.Helper()
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return app
}

func TestWithModuleRegistersChildrenRecursively(t *testing.T) {
	app := newApp(t)
	var order []string

	leaf := recordingModule{name: "leaf", order: &order}
	mid := recordingModule{name: "mid", order: &order, children: []Module{leaf}}
	top := recordingModule{name: "top", order: &order, children: []Module{mid}}

	app.WithModule(top)
	if app.Err() != nil {
		t.Fatalf("WithModule: %v", app.Err())
	}

	want := []string{"top", "mid", "leaf"}
	if len(order) != len(want) {
		t.Fatalf("install order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("install order = %v, want %v", order, want)
		}
	}
}

// distinct module types so dedup (which keys on type) treats them separately.
type moduleA struct{ order *[]string }
type moduleB struct{ order *[]string }

func (m moduleA) Register(*App) error { *m.order = append(*m.order, "A"); return nil }
func (m moduleB) Register(app *App) error {
	*m.order = append(*m.order, "B")
	// Both branches import the same shared module type; it must install once.
	app.WithModule(moduleA{order: m.order})
	return nil
}

func TestWithModuleDedupsByType(t *testing.T) {
	app := newApp(t)
	var order []string

	app.WithModule(moduleB{order: &order}) // imports A
	app.WithModule(moduleA{order: &order}) // same type as the one B imported

	count := 0
	for _, n := range order {
		if n == "A" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("module A installed %d times, want 1 (order=%v)", count, order)
	}
}

func TestModuleFuncIsNotDeduped(t *testing.T) {
	app := newApp(t)
	calls := 0
	fn := ModuleFunc(func(*App) error { calls++; return nil })

	app.WithModule(fn)
	app.WithModule(fn)

	if calls != 2 {
		t.Fatalf("ModuleFunc ran %d times, want 2 (each install is anonymous)", calls)
	}
}

type failingModule struct{}

func (failingModule) Register(*App) error { return errors.New("boom") }

func TestWithModulePropagatesError(t *testing.T) {
	app := newApp(t)
	ran := false

	app.WithModule(failingModule{}).
		WithModule(ModuleFunc(func(*App) error { ran = true; return nil }))

	if app.Err() == nil {
		t.Fatal("expected WithModule to record the module error")
	}
	if ran {
		t.Error("a module ran after an earlier module failed; chain did not short-circuit")
	}
}

// providerService provides another service from inside its own Init, exercising
// the fixpoint drain in initServices: the dynamically added service must still
// be initialized.
type providerService struct{ app *App }

func (s *providerService) Init(app *App) error {
	ProvideService(app, &providedService{})
	return nil
}

type providedService struct{ initialized bool }

func (s *providedService) Init(*App) error { s.initialized = true; return nil }

func TestInitServicesDrainsDynamicallyProvidedServices(t *testing.T) {
	app := newApp(t)
	ProvideService(app, &providerService{})

	if err := app.initServices(); err != nil {
		t.Fatalf("initServices: %v", err)
	}

	provided, ok := ResolveService[*providedService](app)
	if !ok {
		t.Fatal("service provided during Init was never registered")
	}
	if !provided.initialized {
		t.Error("service provided during Init was never initialized; fixpoint drain failed")
	}
}
