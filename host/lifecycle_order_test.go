package host

import (
	"slices"
	"testing"
)

// recordingLifecycle records the phase+name of each lifecycle hook it receives,
// so tests can assert the order services are visited in.
type recordingLifecycle struct {
	name string
	log  *[]string
}

func (r *recordingLifecycle) Init(*App) error  { *r.log = append(*r.log, "init:"+r.name); return nil }
func (r *recordingLifecycle) Setup(*App) error { *r.log = append(*r.log, "setup:"+r.name); return nil }
func (r *recordingLifecycle) Start(*App) error { *r.log = append(*r.log, "start:"+r.name); return nil }

// TestLifecyclePhasesFollowRegistrationOrder asserts init, setup, and start each
// visit services in registration order (A, B, C). It repeats many times because
// the old sync.Map.Range order was random — a single pass could pass by luck.
func TestLifecyclePhasesFollowRegistrationOrder(t *testing.T) {
	for iter := range 50 {
		app := newApp(t)
		var log []string
		ProvideService(app, &recordingLifecycle{name: "A", log: &log}, "A")
		ProvideService(app, &recordingLifecycle{name: "B", log: &log}, "B")
		ProvideService(app, &recordingLifecycle{name: "C", log: &log}, "C")

		if err := app.initServices(); err != nil {
			t.Fatalf("initServices: %v", err)
		}
		if err := app.setupServices(); err != nil {
			t.Fatalf("setupServices: %v", err)
		}
		if err := app.startServices(); err != nil {
			t.Fatalf("startServices: %v", err)
		}

		want := []string{
			"init:A", "init:B", "init:C",
			"setup:A", "setup:B", "setup:C",
			"start:A", "start:B", "start:C",
		}
		if !slices.Equal(log, want) {
			t.Fatalf("iter %d: lifecycle order = %v, want %v", iter, log, want)
		}
	}
}

// TestReprovideKeepsRegistrationPosition asserts that re-providing an existing
// (type, name) replaces the value but keeps the original registration position.
func TestReprovideKeepsRegistrationPosition(t *testing.T) {
	app := newApp(t)
	var log []string
	ProvideService(app, &recordingLifecycle{name: "A", log: &log}, "A")
	ProvideService(app, &recordingLifecycle{name: "B", log: &log}, "B")
	// Re-provide (recordingLifecycle, "A") with a fresh instance; it must stay
	// first and its new value must win.
	ProvideService(app, &recordingLifecycle{name: "A2", log: &log}, "A")

	if err := app.initServices(); err != nil {
		t.Fatalf("initServices: %v", err)
	}

	want := []string{"init:A2", "init:B"}
	if !slices.Equal(log, want) {
		t.Fatalf("init order after re-provide = %v, want %v", log, want)
	}
}

// TestCloseReversesRegistrationWithinBand asserts that services in the same close
// band close in reverse registration order (last registered, first closed).
func TestCloseReversesRegistrationWithinBand(t *testing.T) {
	app := newApp(t)
	var closed []string
	ProvideService(app, &orderedCloser{name: "A", order: CloseDefault, log: &closed}, "A")
	ProvideService(app, &orderedCloser{name: "B", order: CloseDefault, log: &closed}, "B")
	ProvideService(app, &orderedCloser{name: "C", order: CloseDefault, log: &closed}, "C")

	if err := app.closeServices(); err != nil {
		t.Fatalf("closeServices: %v", err)
	}

	want := []string{"C", "B", "A"}
	if !slices.Equal(closed, want) {
		t.Fatalf("close order = %v, want %v", closed, want)
	}
}

// TestCloseOrdersBandsThenReverseWithinBand asserts bands still run ascending
// (edges before backends) while, within each band, services close in reverse
// registration order.
func TestCloseOrdersBandsThenReverseWithinBand(t *testing.T) {
	app := newApp(t)
	var closed []string
	// Registration order interleaves bands so neither pure registration order nor
	// pure band order alone would produce the expected sequence.
	ProvideService(app, &orderedCloser{name: "edge1", order: CloseEdge, log: &closed}, "edge1")
	ProvideService(app, &orderedCloser{name: "backend1", order: CloseBackend, log: &closed}, "backend1")
	ProvideService(app, &orderedCloser{name: "default1", order: CloseDefault, log: &closed}, "default1")
	ProvideService(app, &orderedCloser{name: "edge2", order: CloseEdge, log: &closed}, "edge2")
	ProvideService(app, &orderedCloser{name: "default2", order: CloseDefault, log: &closed}, "default2")

	if err := app.closeServices(); err != nil {
		t.Fatalf("closeServices: %v", err)
	}

	// Edges first (reverse-registration: edge2 before edge1), then defaults
	// (default2 before default1), then backends.
	want := []string{"edge2", "edge1", "default2", "default1", "backend1"}
	if !slices.Equal(closed, want) {
		t.Fatalf("close order = %v, want %v", closed, want)
	}
}
