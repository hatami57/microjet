package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hatami57/microjet/core/errorx"
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
	scope      func(c *gin.Context) string
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

// WithIdempotencyScope overrides how stored responses are partitioned between
// callers. scope returns an identifier for whoever is making the request; the
// same key sent by two callers with different scopes never replays one caller's
// response to the other. The default scopes by the tenant (Tenant middleware)
// and the JWT subject (JWT middleware) when those are present; supply a scope
// when callers are identified some other way, such as API keys or sessions.
func WithIdempotencyScope(scope func(c *gin.Context) string) IdempotencyOption {
	return func(c *idempotencyConfig) {
		if scope != nil {
			c.scope = scope
		}
	}
}

// Idempotency replays the stored response for a repeated request carrying the
// same idempotency key, so a client that retries a non-safe request (after a
// timeout or network blip) gets the original outcome instead of acting twice.
// Requests without the key, or with a safe method, pass through untouched.
//
// The key is scoped by caller, method and request URI (path and query), so the
// same key sent by another caller, to another endpoint, or for another resource
// (/things/1 vs /things/2) does not collide. The caller comes from
// WithIdempotencyScope — by default the tenant and JWT subject, so register
// Idempotency after the Tenant and JWT middlewares. With neither in place and no
// custom scope, every client shares one keyspace, which is only safe on APIs
// with a single principal or trusted clients.
//
// The request body is fingerprinted: reusing a key with a different body is a
// client bug, rejected with 422 Unprocessable Entity rather than answered with
// the response to a request that was never made. The body is read into memory
// to hash it, so cap it with BodyLimit on routes that accept large payloads.
//
// By default only final responses with status < 500 are stored (a 5xx is
// treated as transient and left retryable). A request whose handler recorded an
// error with c.Error is never stored either: the Error middleware renders that
// error only after this middleware returns, so the real status and body are not
// known here, and the retry re-runs the handler instead. Pass an app cache
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
		scope:      defaultIdempotencyScope,
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
		fingerprint, err := fingerprintBody(c.Request)
		if err != nil {
			_ = c.Error(errorx.NewBadRequestError("idempotency", "reading request body failed").WithInner(err))
			c.Abort()
			return
		}
		ctx := c.Request.Context()
		storeKey := idempotencyKey(c, cfg.scope(c), key)

		if data, found, err := store.GetBytes(ctx, storeKey); err == nil && found {
			var stored storedResponse
			if json.Unmarshal(data, &stored) == nil {
				if stored.Fingerprint != fingerprint {
					c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{
						"error":   "idempotency_key_reused",
						"message": "the idempotency key was already used for a request with a different body",
					})
					return
				}
				c.Header(ReplayedHeader, "true")
				c.Data(stored.Status, stored.ContentType, stored.Body)
				c.Abort()
				return
			}
		}

		capture := &responseCapture{ResponseWriter: c.Writer, body: &bytes.Buffer{}}
		c.Writer = capture
		c.Next()

		// An error recorded with c.Error is rendered by the Error middleware after
		// this one returns, so the status and body seen here are not what the
		// client receives. Leave the request unstored; its retry re-runs the handler.
		status := c.Writer.Status()
		if len(c.Errors) > 0 || !cfg.shouldSave(status) {
			return
		}
		stored := storedResponse{
			Status:      status,
			ContentType: c.Writer.Header().Get("Content-Type"),
			Body:        capture.body.Bytes(),
			Fingerprint: fingerprint,
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
	// Fingerprint is the SHA-256 of the request body the response answers.
	Fingerprint string `json:"fingerprint"`
}

// defaultIdempotencyScope identifies the caller by the tenant the Tenant
// middleware resolved and the subject of the token the JWT middleware verified,
// whichever are present. With neither, it is empty: one shared keyspace.
func defaultIdempotencyScope(c *gin.Context) string {
	var tenantID, subject string
	if id, err := GetTenantID(c); err == nil {
		tenantID = id.String()
	}
	if claims, ok := JWTClaimsFromContext(c.Request.Context()); ok {
		subject, _ = claims.GetSubject()
	}
	return tenantID + "/" + subject
}

// idempotencyKey derives the store key from the caller's scope, the method, the
// request URI (so /things/1 and /things/2 never share a stored response) and the
// client key. Each part is length-prefixed so no choice of values can collide
// with another combination, and the result is hashed so the key has a fixed
// length however long the client's header is.
func idempotencyKey(c *gin.Context, scope, key string) string {
	h := sha256.New()
	for _, part := range []string{scope, c.Request.Method, c.Request.URL.RequestURI(), key} {
		_, _ = fmt.Fprintf(h, "%d:%s", len(part), part)
	}
	return "idem:" + hex.EncodeToString(h.Sum(nil))
}

// fingerprintBody returns the SHA-256 of the request body, then restores the
// body so the handler can still read it.
func fingerprintBody(r *http.Request) (string, error) {
	var body []byte
	if r.Body != nil {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return "", err
		}
		body = b
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
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
