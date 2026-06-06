package host

import (
	"errors"
	"testing"
	"time"

	"github.com/hatami57/microjet/core"
)

func TestNewUsesDefaults(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if app.Config == nil || app.Logger == nil {
		t.Fatal("expected Config and Logger to be initialized")
	}
	if app.Clock == nil {
		t.Error("expected a default Clock to be set")
	}
}

func TestWithClockInjectsTimeProvider(t *testing.T) {
	fixed := core.NewFixedClock(time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC))
	app, err := New(WithClock(fixed))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if app.Clock != fixed {
		t.Errorf("Clock = %v, want injected FixedClock", app.Clock)
	}
	if !app.Clock.Now().Equal(fixed.Now()) {
		t.Error("app clock does not report the injected time")
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

	// Setups are deferred until services start; runSetups drives that phase and
	// must stop at the first failing handler.
	if err := app.runSetups(); !errors.Is(err, first) {
		t.Errorf("runSetups() = %v, want %v", err, first)
	}
	if secondCalled {
		t.Error("second Setup ran after the first failed; runSetups did not short-circuit")
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
