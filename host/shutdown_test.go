package host

import (
	"testing"
)

// orderedCloser records the order in which Close is called and reports a
// configurable shutdown band.
type orderedCloser struct {
	name  string
	order int
	log   *[]string
}

func (c *orderedCloser) CloseOrder() int { return c.order }
func (c *orderedCloser) Close(*App) error {
	*c.log = append(*c.log, c.name)
	return nil
}

func TestCloseServicesOrdersEdgesBeforeBackends(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var closed []string
	// Register out of order under distinct names (same type), so the container
	// holds all three; it iterates unordered, so only CloseOrder decides the close
	// sequence.
	backend := &orderedCloser{name: "backend", order: CloseBackend, log: &closed}
	edge := &orderedCloser{name: "edge", order: CloseEdge, log: &closed}
	mid := &orderedCloser{name: "mid", order: CloseDefault, log: &closed}
	ProvideService(app, backend, "backend")
	ProvideService(app, edge, "edge")
	ProvideService(app, mid, "mid")

	if err := app.closeServices(); err != nil {
		t.Fatalf("closeServices: %v", err)
	}

	want := []string{"edge", "mid", "backend"}
	if len(closed) != len(want) {
		t.Fatalf("closed = %v, want %v", closed, want)
	}
	for i := range want {
		if closed[i] != want[i] {
			t.Fatalf("close order = %v, want %v", closed, want)
		}
	}
}

// plainCloser implements only ServiceCloser, not CloseOrderer.
type plainCloser struct{}

func (plainCloser) Close(*App) error { return nil }

func TestUnmarkedServiceClosesAtDefault(t *testing.T) {
	if got := closeOrder(plainCloser{}); got != CloseDefault {
		t.Errorf("closeOrder(unmarked) = %d, want %d", got, CloseDefault)
	}
}
