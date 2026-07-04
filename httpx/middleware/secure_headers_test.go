package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func secureHeadersRouter(cfg SecureHeadersConfig) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecureHeaders(cfg))
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func TestSecureHeadersDefaults(t *testing.T) {
	r := secureHeadersRouter(DefaultSecureHeadersConfig())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want no-referrer", got)
	}
}

func TestSecureHeadersHSTSOnlyOnTLS(t *testing.T) {
	r := secureHeadersRouter(DefaultSecureHeadersConfig())

	// Plain HTTP: no HSTS.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS on plain HTTP = %q, want empty", got)
	}

	// TLS request: HSTS present with the configured directives.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.TLS = &tls.ConnectionState{}
	r.ServeHTTP(rec, req)
	got := rec.Header().Get("Strict-Transport-Security")
	if !strings.HasPrefix(got, "max-age=31536000") || !strings.Contains(got, "includeSubDomains") {
		t.Errorf("HSTS on TLS = %q, want one-year max-age with includeSubDomains", got)
	}
}

func TestSecureHeadersZeroConfigSetsNothing(t *testing.T) {
	r := secureHeadersRouter(SecureHeadersConfig{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	for _, h := range []string{"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "Strict-Transport-Security"} {
		if got := rec.Header().Get(h); got != "" {
			t.Errorf("%s = %q, want empty for zero config", h, got)
		}
	}
}
