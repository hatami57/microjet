package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func corsRouter(cfg CORSConfig) *gin.Engine {
	r := gin.New()
	r.Use(CORS(cfg))
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func TestCORSPreflightAllowed(t *testing.T) {
	r := corsRouter(DefaultCORSConfig())
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("allow-origin = %q, want *", got)
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("missing Access-Control-Allow-Methods on preflight")
	}
}

func TestCORSCredentialsReflectsOrigin(t *testing.T) {
	cfg := DefaultCORSConfig()
	cfg.AllowCredentials = true
	r := corsRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("allow-origin = %q, want reflected origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("allow-credentials = %q, want true", got)
	}
}

func TestCORSDisallowedOriginPreflightRejected(t *testing.T) {
	r := corsRouter(CORSConfig{AllowOrigins: []string{"https://allowed.com"}, AllowMethods: []string{"GET"}})
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("disallowed preflight status = %d, want 403", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("disallowed origin should not receive an allow-origin header")
	}
}

func TestCORSNoOriginPassesThrough(t *testing.T) {
	r := corsRouter(DefaultCORSConfig())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("requests without Origin should not get CORS headers")
	}
}
