package middleware

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// SecureHeadersConfig configures the SecureHeaders middleware. The zero value is
// usable but sets nothing; use DefaultSecureHeadersConfig for sane defaults.
type SecureHeadersConfig struct {
	// ContentTypeNosniff sets X-Content-Type-Options: nosniff when true.
	ContentTypeNosniff bool
	// FrameOptions sets X-Frame-Options (e.g. "DENY", "SAMEORIGIN"); empty omits it.
	FrameOptions string
	// ReferrerPolicy sets Referrer-Policy; empty omits it.
	ReferrerPolicy string
	// ContentSecurityPolicy sets Content-Security-Policy; empty omits it.
	ContentSecurityPolicy string

	// HSTS enables Strict-Transport-Security. The header is only emitted on
	// requests received over TLS, since sending it on plain HTTP is meaningless
	// and can lock clients out if TLS is later misconfigured.
	HSTS bool
	// HSTSMaxAge is the max-age directive (default 365 days when HSTS is on).
	HSTSMaxAge time.Duration
	// HSTSIncludeSubdomains adds includeSubDomains.
	HSTSIncludeSubdomains bool
	// HSTSPreload adds preload (requires includeSubDomains and a long max-age).
	HSTSPreload bool
}

// DefaultSecureHeadersConfig returns a conservative baseline: nosniff on,
// X-Frame-Options DENY, Referrer-Policy no-referrer, and HSTS enabled (emitted
// only on TLS requests) with a one-year max-age including subdomains.
func DefaultSecureHeadersConfig() SecureHeadersConfig {
	return SecureHeadersConfig{
		ContentTypeNosniff:    true,
		FrameOptions:          "DENY",
		ReferrerPolicy:        "no-referrer",
		HSTS:                  true,
		HSTSMaxAge:            365 * 24 * time.Hour,
		HSTSIncludeSubdomains: true,
	}
}

// SecureHeaders returns a middleware that sets common security response headers
// per the given config. It is opt-in, like CORS. HSTS is applied only to
// requests served over TLS.
func SecureHeaders(cfg SecureHeadersConfig) gin.HandlerFunc {
	hstsValue := buildHSTSValue(cfg)

	return func(c *gin.Context) {
		h := c.Writer.Header()
		if cfg.ContentTypeNosniff {
			h.Set("X-Content-Type-Options", "nosniff")
		}
		if cfg.FrameOptions != "" {
			h.Set("X-Frame-Options", cfg.FrameOptions)
		}
		if cfg.ReferrerPolicy != "" {
			h.Set("Referrer-Policy", cfg.ReferrerPolicy)
		}
		if cfg.ContentSecurityPolicy != "" {
			h.Set("Content-Security-Policy", cfg.ContentSecurityPolicy)
		}
		if hstsValue != "" && c.Request.TLS != nil {
			h.Set("Strict-Transport-Security", hstsValue)
		}
		c.Next()
	}
}

// buildHSTSValue precomputes the Strict-Transport-Security header value, or ""
// when HSTS is disabled.
func buildHSTSValue(cfg SecureHeadersConfig) string {
	if !cfg.HSTS {
		return ""
	}
	maxAge := cfg.HSTSMaxAge
	if maxAge <= 0 {
		maxAge = 365 * 24 * time.Hour
	}
	parts := []string{"max-age=" + strconv.Itoa(int(maxAge.Seconds()))}
	if cfg.HSTSIncludeSubdomains {
		parts = append(parts, "includeSubDomains")
	}
	if cfg.HSTSPreload {
		parts = append(parts, "preload")
	}
	return strings.Join(parts, "; ")
}
