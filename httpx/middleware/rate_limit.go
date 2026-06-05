package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// RateLimitConfig configures the per-client rate limiter.
type RateLimitConfig struct {
	// RPS is the sustained requests-per-second allowed per client.
	RPS float64
	// Burst is the maximum burst size (bucket capacity). Defaults to max(1, RPS).
	Burst int
	// KeyFunc identifies the client; defaults to the client IP.
	KeyFunc func(*gin.Context) string
	// IdleTTL is how long an unused client limiter is retained before eviction
	// (default 10m). Bounds memory under many distinct clients.
	IdleTTL time.Duration
}

// RateLimit returns a middleware that throttles each client to the configured
// token-bucket rate, responding 429 with a Retry-After header when exceeded.
// Per-client limiters are evicted after IdleTTL to bound memory.
func RateLimit(cfg RateLimitConfig) gin.HandlerFunc {
	burst := cfg.Burst
	if burst <= 0 {
		burst = max(1, int(cfg.RPS))
	}
	keyFunc := cfg.KeyFunc
	if keyFunc == nil {
		keyFunc = func(c *gin.Context) string { return c.ClientIP() }
	}
	ttl := cfg.IdleTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	store := &limiterStore{
		limiters:    map[string]*clientLimiter{},
		rps:         rate.Limit(cfg.RPS),
		burst:       burst,
		ttl:         ttl,
		lastCleanup: time.Now(),
	}

	return func(c *gin.Context) {
		if !store.get(keyFunc(c)).Allow() {
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":   "too_many_requests",
				"message": "rate limit exceeded",
			})
			return
		}
		c.Next()
	}
}

type clientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type limiterStore struct {
	mu          sync.Mutex
	limiters    map[string]*clientLimiter
	rps         rate.Limit
	burst       int
	ttl         time.Duration
	lastCleanup time.Time
}

func (s *limiterStore) get(key string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	// Opportunistic sweep of idle limiters, so there is no background goroutine
	// to manage or leak.
	if now.Sub(s.lastCleanup) > s.ttl {
		for k, cl := range s.limiters {
			if now.Sub(cl.lastSeen) > s.ttl {
				delete(s.limiters, k)
			}
		}
		s.lastCleanup = now
	}

	cl, ok := s.limiters[key]
	if !ok {
		cl = &clientLimiter{limiter: rate.NewLimiter(s.rps, s.burst)}
		s.limiters[key] = cl
	}
	cl.lastSeen = now
	return cl.limiter
}
