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

type setupperService struct {
	setupCalls int
	err        error
}

func (s *setupperService) Setup(*App) error {
	s.setupCalls++
	return s.err
}

func TestSetupServicesRunsHookBeforeQueuedSetups(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var order []string
	svc := &setupperService{}
	ProvideService(app, svc)
	app.Setup(func(*App) error { order = append(order, "queued"); return nil })

	// setupServices runs the service's own Setup hook; the queued app.Setup
	// handler must observe it, so the service hook runs first.
	if err := app.setupServices(); err != nil {
		t.Fatalf("setupServices: %v", err)
	}
	if svc.setupCalls != 1 {
		t.Fatalf("Setup called %d times, want 1", svc.setupCalls)
	}
	order = append(order, "afterSetupServices")
	if err := app.runSetups(); err != nil {
		t.Fatalf("runSetups: %v", err)
	}

	want := []string{"afterSetupServices", "queued"}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] {
		t.Errorf("order = %v, want %v", order, want)
	}
}

func TestSetupServicesIsIdempotent(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	svc := &setupperService{}
	ProvideService(app, svc)

	for range 3 {
		if err := app.setupServices(); err != nil {
			t.Fatalf("setupServices: %v", err)
		}
	}
	if svc.setupCalls != 1 {
		t.Errorf("Setup called %d times across repeated setupServices, want 1", svc.setupCalls)
	}
}

func TestSetupServicesPropagatesError(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	boom := errors.New("setup failed")
	svc := &setupperService{err: boom}
	ProvideService(app, svc)

	if err := app.setupServices(); !errors.Is(err, boom) {
		t.Fatalf("setupServices() = %v, want %v", err, boom)
	}
	if app.isServiceSetup {
		t.Error("isServiceSetup set despite a failing Setup hook")
	}
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
