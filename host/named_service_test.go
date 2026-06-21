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
