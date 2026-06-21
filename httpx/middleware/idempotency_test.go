package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// memStore is a tiny in-memory IdempotencyStore for tests.
type memStore struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newMemStore() *memStore { return &memStore{m: map[string][]byte{}} }

func (s *memStore) GetBytes(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	return v, ok, nil
}

func (s *memStore) SetBytes(_ context.Context, key string, value []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = value
	return nil
}

// countingRouter builds a router whose POST /things handler increments calls and
// echoes the count, so a replayed response is detectable.
func idempotentRouter(store IdempotencyStore, calls *int, opts ...IdempotencyOption) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Idempotency(store, opts...))
	r.POST("/things", func(c *gin.Context) {
		*calls++
		c.JSON(http.StatusCreated, gin.H{"count": *calls})
	})
	r.GET("/things", func(c *gin.Context) {
		*calls++
		c.JSON(http.StatusOK, gin.H{"count": *calls})
	})
	return r
}

func do(r *gin.Engine, method, path, key string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	if key != "" {
		req.Header.Set(DefaultIdempotencyHeader, key)
	}
	r.ServeHTTP(w, req)
	return w
}

func TestIdempotencyReplaysSameKey(t *testing.T) {
	calls := 0
	r := idempotentRouter(newMemStore(), &calls)

	first := do(r, http.MethodPost, "/things", "k1")
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d", first.Code)
	}
	if first.Header().Get(ReplayedHeader) != "" {
		t.Error("first response should not be marked replayed")
	}

	second := do(r, http.MethodPost, "/things", "k1")
	if second.Code != http.StatusCreated {
		t.Errorf("replayed status = %d, want 201", second.Code)
	}
	if second.Header().Get(ReplayedHeader) != "true" {
		t.Error("second response should be marked replayed")
	}
	if first.Body.String() != second.Body.String() {
		t.Errorf("bodies differ: %q vs %q", first.Body.String(), second.Body.String())
	}
	if calls != 1 {
		t.Errorf("handler calls = %d, want 1 (second served from store)", calls)
	}
}

func TestIdempotencyDistinctKeysRunHandler(t *testing.T) {
	calls := 0
	r := idempotentRouter(newMemStore(), &calls)
	do(r, http.MethodPost, "/things", "a")
	do(r, http.MethodPost, "/things", "b")
	if calls != 2 {
		t.Errorf("handler calls = %d, want 2", calls)
	}
}

func TestIdempotencyNoKeyPassesThrough(t *testing.T) {
	calls := 0
	r := idempotentRouter(newMemStore(), &calls)
	do(r, http.MethodPost, "/things", "")
	do(r, http.MethodPost, "/things", "")
	if calls != 2 {
		t.Errorf("handler calls = %d, want 2 (no key, no replay)", calls)
	}
}

func TestIdempotencyIgnoresSafeMethods(t *testing.T) {
	calls := 0
	r := idempotentRouter(newMemStore(), &calls)
	do(r, http.MethodGet, "/things", "same")
	do(r, http.MethodGet, "/things", "same")
	if calls != 2 {
		t.Errorf("GET handler calls = %d, want 2 (safe method not idempotency-cached)", calls)
	}
}

func TestIdempotencyDoesNotCacheServerErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := 0
	store := newMemStore()
	r := gin.New()
	r.Use(Idempotency(store))
	r.POST("/flaky", func(c *gin.Context) {
		calls++
		if calls == 1 {
			c.JSON(http.StatusBadGateway, gin.H{"error": "upstream"})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})

	if w := do(r, http.MethodPost, "/flaky", "k"); w.Code != http.StatusBadGateway {
		t.Fatalf("first status = %d, want 502", w.Code)
	}
	// 5xx is not stored, so the retry re-runs the handler and now succeeds.
	if w := do(r, http.MethodPost, "/flaky", "k"); w.Code != http.StatusCreated {
		t.Errorf("retry status = %d, want 201", w.Code)
	}
	if calls != 2 {
		t.Errorf("handler calls = %d, want 2", calls)
	}
}

func TestIdempotencyScopesKeyByRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMemStore()
	var a, b int
	r := gin.New()
	r.Use(Idempotency(store))
	r.POST("/a", func(c *gin.Context) { a++; c.String(http.StatusOK, "a"+strconv.Itoa(a)) })
	r.POST("/b", func(c *gin.Context) { b++; c.String(http.StatusOK, "b"+strconv.Itoa(b)) })

	// Same key on different routes must not collide.
	wa := do(r, http.MethodPost, "/a", "shared")
	wb := do(r, http.MethodPost, "/b", "shared")
	if wa.Body.String() != "a1" || wb.Body.String() != "b1" {
		t.Errorf("route-scoped keys collided: %q %q", wa.Body.String(), wb.Body.String())
	}
}
