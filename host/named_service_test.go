package host

import "testing"

type widget struct{ id string }

func TestNamedServicesCoexist(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	def := &widget{id: "default"}
	admin := &widget{id: "admin"}
	ProvideService(app, def)
	ProvideService(app, admin, "admin")

	if got, _ := ResolveService[*widget](app); got != def {
		t.Errorf("default = %v, want %v", got, def)
	}
	if got, _ := ResolveService[*widget](app, ""); got != def {
		t.Error(`name "" should resolve the default instance`)
	}
	if got, _ := ResolveService[*widget](app, "admin"); got != admin {
		t.Errorf(`name "admin" = %v, want %v`, got, admin)
	}
	if _, ok := ResolveService[*widget](app, "missing"); ok {
		t.Error("unknown name should not resolve")
	}
}

// greeter is an interface with several implementors, exercising the multi-
// registration path (register many under one interface type, resolve by criteria).
type greeter interface{ greet() string }

type englishGreeter struct{}

func (englishGreeter) greet() string { return "hello" }

type frenchGreeter struct{}

func (frenchGreeter) greet() string { return "bonjour" }

func TestResolveAllServices(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ProvideService[greeter](app, englishGreeter{}, "en")
	ProvideService[greeter](app, frenchGreeter{}, "fr")

	all := ResolveAllServices[greeter](app)
	if len(all) != 2 {
		t.Fatalf("ResolveAllServices returned %d services, want 2: %v", len(all), all)
	}
	if all["en"].greet() != "hello" || all["fr"].greet() != "bonjour" {
		t.Errorf("unexpected instances: %+v", all)
	}
}

func TestResolveAllServicesEmptyIsNonNil(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	all := ResolveAllServices[greeter](app)
	if all == nil {
		t.Fatal("ResolveAllServices returned nil, want empty non-nil map")
	}
	if len(all) != 0 {
		t.Errorf("len = %d, want 0", len(all))
	}
}

func TestResolveAllServicesIsTypeExact(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Registered under the concrete type, not the interface: it must NOT surface
	// through an interface query — resolution is exact-type, not by assignability.
	ProvideService(app, englishGreeter{})
	if all := ResolveAllServices[greeter](app); len(all) != 0 {
		t.Errorf("concrete registration leaked into interface query: %v", all)
	}
}

func TestResolveServiceBy(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ProvideService[greeter](app, englishGreeter{}, "en")
	ProvideService[greeter](app, frenchGreeter{}, "fr")

	got, ok := ResolveServiceBy(app, func(g greeter) bool {
		return g.greet() == "bonjour"
	})
	if !ok {
		t.Fatal("ResolveServiceBy found no match, want the french greeter")
	}
	if got.greet() != "bonjour" {
		t.Errorf("got %q, want bonjour", got.greet())
	}

	if _, ok := ResolveServiceBy(app, func(greeter) bool { return false }); ok {
		t.Error("ResolveServiceBy matched when predicate never returns true")
	}
}

func TestMustResolveUnknownNamePanics(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Error("expected panic for missing named service")
		}
	}()
	MustResolveService[*widget](app, "nope")
}
