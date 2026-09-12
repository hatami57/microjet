package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/hatami57/microjet/core/errorx"
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

// doReq sends a POST carrying the idempotency key, body, and extra header pairs.
func doReq(r *gin.Engine, path, key, body string, headers ...string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set(DefaultIdempotencyHeader, key)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	r.ServeHTTP(w, req)
	return w
}

// TestIdempotencyDoesNotStoreRecordedErrors verifies a request whose handler
// recorded an error with c.Error is left unstored. The Error middleware renders
// it only after Idempotency returns, so storing it saved an empty 200.
func TestIdempotencyDoesNotStoreRecordedErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := 0
	r := gin.New()
	r.Use(Error(false), Idempotency(newMemStore()))
	r.POST("/things", func(c *gin.Context) {
		calls++
		if calls == 1 {
			_ = c.Error(errorx.ErrNotFound)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})

	if w := do(r, http.MethodPost, "/things", "k"); w.Code != http.StatusNotFound {
		t.Fatalf("first status = %d, want 404", w.Code)
	}
	second := do(r, http.MethodPost, "/things", "k")
	if second.Code != http.StatusCreated || second.Header().Get(ReplayedHeader) != "" {
		t.Errorf("retry = %d (replayed %q), want a fresh 201", second.Code, second.Header().Get(ReplayedHeader))
	}
	if calls != 2 {
		t.Errorf("handler calls = %d, want 2", calls)
	}
}

func TestIdempotencyScopesKeyByPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := 0
	r := gin.New()
	r.Use(Idempotency(newMemStore()))
	r.POST("/things/:id", func(c *gin.Context) { calls++; c.String(http.StatusOK, c.Param("id")) })

	one := doReq(r, "/things/1", "shared", "")
	two := doReq(r, "/things/2", "shared", "")
	if one.Body.String() != "1" || two.Body.String() != "2" {
		t.Errorf("resources 1 and 2 got %q and %q, want 1 and 2", one.Body.String(), two.Body.String())
	}
	if again := doReq(r, "/things/1", "shared", ""); again.Header().Get(ReplayedHeader) != "true" {
		t.Error("retry for resource 1 should replay its stored response")
	}
	if calls != 2 {
		t.Errorf("handler calls = %d, want 2", calls)
	}
}

// TestIdempotencyScopesKeyByCaller verifies the default scope keeps tenants and
// JWT subjects apart: the same key from another caller never replays a response.
func TestIdempotencyScopesKeyByCaller(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := 0
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if id := c.GetHeader("X-Test-Tenant"); id != "" {
			c.Set(TenantIDContextKey, uuid.MustParse(id))
		}
		if sub := c.GetHeader("X-Test-Subject"); sub != "" {
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), claimsKey, jwt.MapClaims{"sub": sub}))
		}
		c.Next()
	}, Idempotency(newMemStore()))
	r.POST("/things", func(c *gin.Context) { calls++; c.String(http.StatusOK, strconv.Itoa(calls)) })

	send := func(tenantID, subject string) string {
		return doReq(r, "/things", "k", "", "X-Test-Tenant", tenantID, "X-Test-Subject", subject).Body.String()
	}
	t1, t2 := uuid.NewString(), uuid.NewString()
	if got := send(t1, "alice"); got != "1" {
		t.Fatalf("first request = %q, want 1", got)
	}
	if got := send(t1, "alice"); got != "1" {
		t.Errorf("same caller's retry = %q, want replayed 1", got)
	}
	if got := send(t2, "alice"); got != "2" {
		t.Errorf("another tenant got %q, want its own response 2", got)
	}
	if got := send(t1, "bob"); got != "3" {
		t.Errorf("another subject got %q, want its own response 3", got)
	}
}

func TestIdempotencyCustomScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := 0
	r := gin.New()
	r.Use(Idempotency(newMemStore(), WithIdempotencyScope(func(c *gin.Context) string { return c.GetHeader("X-Api-Key") })))
	r.POST("/things", func(c *gin.Context) { calls++; c.String(http.StatusOK, strconv.Itoa(calls)) })

	a := doReq(r, "/things", "k", "", "X-Api-Key", "a")
	b := doReq(r, "/things", "k", "", "X-Api-Key", "b")
	if a.Body.String() != "1" || b.Body.String() != "2" {
		t.Errorf("callers a and b got %q and %q, want 1 and 2", a.Body.String(), b.Body.String())
	}
}

// TestIdempotencyRejectsKeyReuseWithDifferentBody verifies the body fingerprint:
// the same body replays, a different one is rejected with 422, and the handler
// still reads the body the fingerprint consumed.
func TestIdempotencyRejectsKeyReuseWithDifferentBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := 0
	r := gin.New()
	r.Use(Idempotency(newMemStore()))
	r.POST("/charges", func(c *gin.Context) {
		calls++
		body, _ := io.ReadAll(c.Request.Body)
		c.String(http.StatusCreated, string(body))
	})

	first := doReq(r, "/charges", "k", `{"amount":1}`)
	if first.Code != http.StatusCreated || first.Body.String() != `{"amount":1}` {
		t.Fatalf("first = %d %q, want 201 echoing the body", first.Code, first.Body.String())
	}
	if same := doReq(r, "/charges", "k", `{"amount":1}`); same.Header().Get(ReplayedHeader) != "true" {
		t.Error("retry with the same body should replay")
	}
	if diff := doReq(r, "/charges", "k", `{"amount":2}`); diff.Code != http.StatusUnprocessableEntity {
		t.Errorf("reuse with a different body = %d, want 422", diff.Code)
	}
	if calls != 1 {
		t.Errorf("handler calls = %d, want 1", calls)
	}
}

func TestIdempotencyStoreKeysAreFixedLengthHashes(t *testing.T) {
	calls := 0
	store := newMemStore()
	r := idempotentRouter(store, &calls)
	do(r, http.MethodPost, "/things", "short")
	do(r, http.MethodPost, "/things", strings.Repeat("k", 4096))

	if len(store.m) != 2 {
		t.Fatalf("stored %d responses, want 2", len(store.m))
	}
	for k := range store.m {
		if len(k) != len("idem:")+64 || strings.Contains(k, "short") {
			t.Errorf("store key %q is not a fixed-length hash", k)
		}
	}
}
