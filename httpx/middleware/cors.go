package middleware

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// CORSConfig configures the CORS middleware.
type CORSConfig struct {
	// AllowOrigins is the list of permitted origins. Use ["*"] to allow any
	// origin. When AllowCredentials is true a wildcard is reflected back as the
	// requesting origin (the spec forbids "*" with credentials).
	AllowOrigins []string
	// AllowMethods lists methods permitted on cross-origin requests.
	AllowMethods []string
	// AllowHeaders lists request headers the client may send.
	AllowHeaders []string
	// ExposeHeaders lists response headers the browser may read.
	ExposeHeaders []string
	// AllowCredentials allows cookies/authorization on cross-origin requests.
	AllowCredentials bool
	// MaxAge is how long a preflight result may be cached.
	MaxAge time.Duration
}

// DefaultCORSConfig returns a permissive-but-sane configuration: any origin, the
// common methods, and the headers microjet handlers typically need.
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowOrigins:  []string{"*"},
		AllowMethods:  []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:  []string{"Origin", "Content-Type", "Accept", "Authorization", DefaultRequestIDHeader},
		ExposeHeaders: []string{DefaultRequestIDHeader},
		MaxAge:        12 * time.Hour,
	}
}

// CORS returns a middleware that applies the given cross-origin policy and
// answers preflight (OPTIONS) requests with 204. Requests without an Origin
// header pass through untouched.
func CORS(cfg CORSConfig) gin.HandlerFunc {
	allowAll := false
	origins := make(map[string]bool, len(cfg.AllowOrigins))
	for _, o := range cfg.AllowOrigins {
		if o == "*" {
			allowAll = true
		}
		origins[o] = true
	}
	allowMethods := strings.Join(cfg.AllowMethods, ", ")
	allowHeaders := strings.Join(cfg.AllowHeaders, ", ")
	exposeHeaders := strings.Join(cfg.ExposeHeaders, ", ")
	maxAge := strconv.Itoa(int(cfg.MaxAge.Seconds()))

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}

		allowOrigin, ok := resolveCORSOrigin(origin, allowAll, cfg.AllowCredentials, origins)
		if !ok {
			// Origin not permitted: reject preflight, let actual requests proceed
			// without CORS headers (the browser will block the response).
			if c.Request.Method == "OPTIONS" {
				c.AbortWithStatus(403)
				return
			}
			c.Next()
			return
		}

		header := c.Writer.Header()
		header.Set("Access-Control-Allow-Origin", allowOrigin)
		if allowOrigin != "*" {
			header.Add("Vary", "Origin")
		}
		if cfg.AllowCredentials {
			header.Set("Access-Control-Allow-Credentials", "true")
		}
		if exposeHeaders != "" {
			header.Set("Access-Control-Expose-Headers", exposeHeaders)
		}

		if c.Request.Method == "OPTIONS" {
			if allowMethods != "" {
				header.Set("Access-Control-Allow-Methods", allowMethods)
			}
			if allowHeaders != "" {
				header.Set("Access-Control-Allow-Headers", allowHeaders)
			}
			if cfg.MaxAge > 0 {
				header.Set("Access-Control-Max-Age", maxAge)
			}
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// resolveCORSOrigin decides the Access-Control-Allow-Origin value for a request.
// With credentials a wildcard must be reflected as the concrete origin.
func resolveCORSOrigin(origin string, allowAll, credentials bool, origins map[string]bool) (string, bool) {
	if allowAll {
		if credentials {
			return origin, true
		}
		return "*", true
	}
	if origins[origin] {
		return origin, true
	}
	return "", false
}
