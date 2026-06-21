package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// DefaultIdempotencyHeader is the request header carrying the idempotency key.
const DefaultIdempotencyHeader = "Idempotency-Key"

// DefaultIdempotencyTTL is how long a stored response is replayed for.
const DefaultIdempotencyTTL = 24 * time.Hour

// ReplayedHeader is set to "true" on responses served from a stored result.
const ReplayedHeader = "Idempotent-Replayed"

// IdempotencyStore persists responses keyed by idempotency key. Its method set
// is a subset of cache.Cache, so the app cache — cache.Of(app) — satisfies it
// directly with no adapter, and this package imposes no dependency on the cache
// module.
type IdempotencyStore interface {
	GetBytes(ctx context.Context, key string) (value []byte, found bool, err error)
	SetBytes(ctx context.Context, key string, value []byte, ttl time.Duration) error
}

type idempotencyConfig struct {
	header     string
	ttl        time.Duration
	methods    map[string]bool
	shouldSave func(status int) bool
}

// IdempotencyOption configures the Idempotency middleware.
type IdempotencyOption func(*idempotencyConfig)

// WithIdempotencyHeader overrides the header carrying the key (default
// "Idempotency-Key").
func WithIdempotencyHeader(header string) IdempotencyOption {
	return func(c *idempotencyConfig) {
		if header != "" {
			c.header = header
		}
	}
}

// WithIdempotencyTTL overrides how long stored responses are replayed.
func WithIdempotencyTTL(ttl time.Duration) IdempotencyOption {
	return func(c *idempotencyConfig) {
		if ttl > 0 {
			c.ttl = ttl
		}
	}
}

// WithIdempotencyMethods overrides which HTTP methods are made idempotent
// (default POST, PUT, PATCH, DELETE). Safe methods are already idempotent.
func WithIdempotencyMethods(methods ...string) IdempotencyOption {
	return func(c *idempotencyConfig) {
		c.methods = map[string]bool{}
		for _, m := range methods {
			c.methods[m] = true
		}
	}
}

// Idempotency replays the stored response for a repeated request carrying the
// same idempotency key, so a client that retries a non-safe request (after a
// timeout or network blip) gets the original outcome instead of acting twice.
// Requests without the key, or with a safe method, pass through untouched.
//
// The key is scoped by method and route so the same key on different endpoints
// does not collide. By default only final responses with status < 500 are
// stored (a 5xx is treated as transient and left retryable). Pass an app cache
// (app.Cache()) or any IdempotencyStore.
//
// Note: replay is best-effort across concurrent in-flight duplicates — two
// requests with the same key arriving simultaneously may both execute before
// either response is stored. It protects against sequential retries, the common
// case.
func Idempotency(store IdempotencyStore, opts ...IdempotencyOption) gin.HandlerFunc {
	cfg := idempotencyConfig{
		header:     DefaultIdempotencyHeader,
		ttl:        DefaultIdempotencyTTL,
		methods:    map[string]bool{http.MethodPost: true, http.MethodPut: true, http.MethodPatch: true, http.MethodDelete: true},
		shouldSave: func(status int) bool { return status < 500 },
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return func(c *gin.Context) {
		if !cfg.methods[c.Request.Method] {
			c.Next()
			return
		}
		key := c.GetHeader(cfg.header)
		if key == "" {
			c.Next()
			return
		}
		ctx := c.Request.Context()
		storeKey := idempotencyKey(c, key)

		if data, found, err := store.GetBytes(ctx, storeKey); err == nil && found {
			var stored storedResponse
			if json.Unmarshal(data, &stored) == nil {
				c.Header(ReplayedHeader, "true")
				c.Data(stored.Status, stored.ContentType, stored.Body)
				c.Abort()
				return
			}
		}

		capture := &responseCapture{ResponseWriter: c.Writer, body: &bytes.Buffer{}}
		c.Writer = capture
		c.Next()

		status := c.Writer.Status()
		if !cfg.shouldSave(status) {
			return
		}
		stored := storedResponse{
			Status:      status,
			ContentType: c.Writer.Header().Get("Content-Type"),
			Body:        capture.body.Bytes(),
		}
		if data, err := json.Marshal(stored); err == nil {
			// Best-effort: a store failure must not fail the already-served request.
			_ = store.SetBytes(ctx, storeKey, data, cfg.ttl)
		}
	}
}

// storedResponse is the persisted form of a completed response.
type storedResponse struct {
	Status      int    `json:"status"`
	ContentType string `json:"contentType"`
	Body        []byte `json:"body"`
}

// idempotencyKey scopes the client key by method and matched route so the same
// key used against different endpoints does not collide.
func idempotencyKey(c *gin.Context, key string) string {
	route := c.FullPath()
	if route == "" {
		route = c.Request.URL.Path
	}
	return "idem:" + c.Request.Method + " " + route + ":" + key
}

// responseCapture tees the response body into a buffer while still writing it to
// the client, so a successful response can be stored for replay.
type responseCapture struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *responseCapture) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *responseCapture) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}
