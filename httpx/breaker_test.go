package httpx

import (
	"errors"
	"testing"
	"time"
)

func TestBreakerOpensAfterThreshold(t *testing.T) {
	b := newCircuitBreaker(3, time.Minute)
	for i := range 3 {
		if !b.allow() {
			t.Fatalf("request %d denied before threshold", i)
		}
		b.record(false)
	}
	if b.allow() {
		t.Error("breaker should be open after reaching the failure threshold")
	}
}

func TestBreakerSuccessResetsFailures(t *testing.T) {
	b := newCircuitBreaker(3, time.Minute)
	b.record(false)
	b.record(false)
	b.record(true) // resets the counter
	b.record(false)
	b.record(false)
	if !b.allow() {
		t.Error("breaker opened too early; a success should have reset the counter")
	}
}

func TestBreakerHalfOpenAfterCooldown(t *testing.T) {
	now := time.Now()
	b := newCircuitBreaker(1, 30*time.Second)
	b.now = func() time.Time { return now }

	b.record(false) // trips open
	if b.allow() {
		t.Fatal("breaker should be open immediately after tripping")
	}

	now = now.Add(31 * time.Second) // cooldown elapsed
	if !b.allow() {
		t.Fatal("breaker should admit a trial request after cooldown")
	}
	// Only one trial is admitted while half-open.
	if b.allow() {
		t.Error("breaker admitted a second concurrent trial while half-open")
	}
}

func TestBreakerHalfOpenSuccessCloses(t *testing.T) {
	now := time.Now()
	b := newCircuitBreaker(1, 10*time.Second)
	b.now = func() time.Time { return now }
	b.record(false)
	now = now.Add(11 * time.Second)

	if !b.allow() {
		t.Fatal("expected a trial request")
	}
	b.record(true) // trial succeeds → closed
	if !b.allow() {
		t.Error("breaker should be closed after a successful trial")
	}
}

func TestBreakerHalfOpenFailureReopens(t *testing.T) {
	now := time.Now()
	b := newCircuitBreaker(1, 10*time.Second)
	b.now = func() time.Time { return now }
	b.record(false)
	now = now.Add(11 * time.Second)

	if !b.allow() {
		t.Fatal("expected a trial request")
	}
	b.record(false) // trial fails → reopen
	if b.allow() {
		t.Error("breaker should reopen after a failed trial")
	}
	now = now.Add(11 * time.Second) // cooldown restarts from the reopen
	if !b.allow() {
		t.Error("breaker should admit another trial after the new cooldown")
	}
}

func TestServerFailedClassification(t *testing.T) {
	cases := []struct {
		status int
		err    error
		want   bool
	}{
		{200, nil, false},
		{404, errors.New("not found"), false},  // client error: upstream healthy
		{500, errors.New("boom"), true},        // server error
		{503, errors.New("unavailable"), true}, // server error
		{0, errors.New("dial tcp"), true},      // transport error
	}
	for _, tc := range cases {
		if got := serverFailed(tc.status, tc.err); got != tc.want {
			t.Errorf("serverFailed(%d, %v) = %v, want %v", tc.status, tc.err, got, tc.want)
		}
	}
}
