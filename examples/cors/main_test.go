package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// router mounts the given CORS policy engine-wide over a single GET /data route.
// No OPTIONS handler is registered — preflight is handled by the middleware.
func router(cors gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(cors)
	r.GET("/data", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func do(cors gin.HandlerFunc, method, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/data", nil)
	req.Header.Set("Origin", origin)
	if method == http.MethodOptions {
		req.Header.Set("Access-Control-Request-Method", "GET")
	}
	rec := httptest.NewRecorder()
	router(cors).ServeHTTP(rec, req)
	return rec
}

func TestAllowAllReflectsAnyOrigin(t *testing.T) {
	rec := do(corsAllowAll(), http.MethodGet, "https://anything.example")
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow-origin = %q, want *", got)
	}
}

func TestRestrictedReflectsAllowedOrigin(t *testing.T) {
	rec := do(corsRestricted(), http.MethodGet, "https://app.example.com")
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("allow-origin = %q, want the request origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow-credentials = %q, want true", got)
	}
}

func TestRestrictedRejectsDisallowedOrigin(t *testing.T) {
	rec := do(corsRestricted(), http.MethodGet, "https://evil.example")
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("allow-origin = %q, want empty (origin not permitted)", got)
	}
}

// TestPreflightHandledAutomatically is the point of the engine-wide wiring: an
// OPTIONS preflight is answered (204 allowed, 403 rejected) without any manually
// registered OPTIONS handler.
func TestPreflightHandledAutomatically(t *testing.T) {
	if rec := do(corsAllowAll(), http.MethodOptions, "https://x.example"); rec.Code != http.StatusNoContent {
		t.Fatalf("allow-all preflight = %d, want 204", rec.Code)
	}
	if rec := do(corsRestricted(), http.MethodOptions, "https://app.example.com"); rec.Code != http.StatusNoContent {
		t.Fatalf("restricted allowed preflight = %d, want 204", rec.Code)
	}
	if rec := do(corsRestricted(), http.MethodOptions, "https://evil.example"); rec.Code != http.StatusForbidden {
		t.Fatalf("restricted disallowed preflight = %d, want 403", rec.Code)
	}
}
