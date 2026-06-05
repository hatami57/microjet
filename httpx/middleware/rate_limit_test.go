package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRateLimitBlocksOverBurst(t *testing.T) {
	r := gin.New()
	// 1 rps, burst 1: the first request passes, the immediate second is limited.
	r.Use(RateLimit(RateLimitConfig{RPS: 1, Burst: 1}))
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	do := func() int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := do(); got != http.StatusOK {
		t.Fatalf("first request = %d, want 200", got)
	}
	if got := do(); got != http.StatusTooManyRequests {
		t.Errorf("second request = %d, want 429", got)
	}
}

func TestRateLimitIsPerClient(t *testing.T) {
	r := gin.New()
	r.Use(RateLimit(RateLimitConfig{RPS: 1, Burst: 1}))
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	do := func(ip string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip + ":1234"
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := do("10.0.0.1"); got != http.StatusOK {
		t.Fatalf("client A first = %d, want 200", got)
	}
	// A different client has its own bucket and should still pass.
	if got := do("10.0.0.2"); got != http.StatusOK {
		t.Errorf("client B first = %d, want 200", got)
	}
}
