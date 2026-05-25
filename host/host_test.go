package host

import (
	"errors"
	"testing"
)

func TestNewUsesDefaults(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if app.Config == nil || app.Logger == nil {
		t.Fatal("expected Config and Logger to be initialized")
	}
	if app.Config.Server.Port != 8080 {
		t.Errorf("default port = %d, want 8080", app.Config.Server.Port)
	}
}

func TestSetupCapturesFirstErrorAndShortCircuits(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	first := errors.New("first failure")
	secondCalled := false

	app.Setup(func(a *App) error { return first }).
		Setup(func(a *App) error { secondCalled = true; return nil })

	if !errors.Is(app.Err(), first) {
		t.Errorf("Err() = %v, want %v", app.Err(), first)
	}
	if secondCalled {
		t.Error("second Setup ran after the first failed; chain did not short-circuit")
	}
}

type fakeService struct {
	name string
}

func TestProvideAndResolveService(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ProvideService(app, &fakeService{name: "users"})

	svc, ok := ResolveService[*fakeService](app)
	if !ok || svc.name != "users" {
		t.Fatalf("ResolveService = %+v, %v", svc, ok)
	}

	if _, ok := ResolveService[*App](app); ok {
		t.Error("resolved a type that was never provided")
	}
}

func TestMustResolveServicePanicsWhenMissing(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Error("expected panic for missing service")
		}
	}()
	MustResolveService[*fakeService](app)
}
