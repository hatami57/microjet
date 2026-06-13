package httpx

import (
	"sync"
	"time"
)

// DefaultBreakerThreshold is the number of consecutive server-side failures that
// trips the breaker open.
const DefaultBreakerThreshold = 5

// DefaultBreakerCooldown is how long the breaker stays open before allowing a
// trial request.
const DefaultBreakerCooldown = 30 * time.Second

type breakerState int

const (
	breakerClosed   breakerState = iota // requests flow normally
	breakerOpen                         // requests fail fast
	breakerHalfOpen                     // a single trial request is allowed
)

// circuitBreaker is a per-Client consecutive-failure breaker. It opens after
// threshold consecutive server-side failures (transport errors and 5xx — a 4xx
// means the upstream is healthy and does not count), fails fast while open, and
// after cooldown admits one trial request: success closes it, failure reopens
// it. Safe for concurrent use.
type circuitBreaker struct {
	threshold int
	cooldown  time.Duration
	now       func() time.Time

	mu          sync.Mutex
	state       breakerState
	failures    int
	openedAt    time.Time
	trialActive bool // a half-open trial is in flight
}

func newCircuitBreaker(threshold int, cooldown time.Duration) *circuitBreaker {
	if threshold <= 0 {
		threshold = DefaultBreakerThreshold
	}
	if cooldown <= 0 {
		cooldown = DefaultBreakerCooldown
	}
	return &circuitBreaker{threshold: threshold, cooldown: cooldown, now: time.Now}
}

// allow reports whether a request may proceed. While open it returns false until
// the cooldown elapses, after which it admits a single half-open trial.
func (b *circuitBreaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case breakerOpen:
		if b.now().Sub(b.openedAt) >= b.cooldown {
			b.state = breakerHalfOpen
			b.trialActive = true
			return true
		}
		return false
	case breakerHalfOpen:
		if !b.trialActive {
			b.trialActive = true
			return true
		}
		return false
	default: // breakerClosed
		return true
	}
}

// record updates the breaker with the outcome of a request that allow admitted.
func (b *circuitBreaker) record(success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if success {
		b.failures = 0
		b.state = breakerClosed
		b.trialActive = false
		return
	}
	if b.state == breakerHalfOpen {
		// The trial failed: reopen and restart the cooldown.
		b.state = breakerOpen
		b.openedAt = b.now()
		b.trialActive = false
		return
	}
	b.failures++
	if b.failures >= b.threshold {
		b.state = breakerOpen
		b.openedAt = b.now()
	}
}

// serverFailed reports whether an attempt's outcome is a server-side failure for
// breaker accounting: a transport error (status 0) or a 5xx. A 4xx (or success)
// is not counted, since the upstream is responding.
func serverFailed(status int, err error) bool {
	return err != nil && (status == 0 || status >= 500)
}
