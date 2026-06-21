// Command cors demonstrates the CORS middleware (httpx/middleware.CORS) and its
// two common policies — allow any origin, or restrict to a specific allowlist.
//
// The simplest wiring is engine-wide: r.Use(middleware.CORS(cfg)) runs for every
// request, including the browser's preflight (OPTIONS), so you never register an
// OPTIONS handler yourself. The trade-off is a single policy for the whole
// server; for different policies per route group, attach middleware.CORS to each
// group and add an OPTIONS route there so the group middleware runs for preflight.
//
// This server applies the restricted policy. Swap corsRestricted() for
// corsAllowAll() below to allow any origin.
//
// Run it:
//
//	go run .
//
// Try it (watch the Access-Control-Allow-Origin response header):
//
//	# a permitted origin is reflected back, credentials allowed
//	curl -si localhost:8080/data -H 'Origin: https://app.example.com' | grep -i access-control
//
//	# a disallowed origin gets no CORS header (the browser blocks the response)
//	curl -si localhost:8080/data -H 'Origin: https://evil.example' | grep -i access-control-allow-origin
//
//	# preflight is answered automatically: 204 for an allowed origin, 403 otherwise
//	curl -si -X OPTIONS localhost:8080/data \
//	  -H 'Origin: https://app.example.com' -H 'Access-Control-Request-Method: GET' | head -1
package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hatami57/microjet/host"
	"github.com/hatami57/microjet/httpx"
	"github.com/hatami57/microjet/httpx/middleware"
)

func main() {
	host.MustNew().
		WithModule(httpx.Module()).
		Setup(func(a *host.App) error {
			r := httpx.Of(a).Router

			// Engine-wide CORS: applies to every route and answers preflight
			// automatically — no manual OPTIONS handler. Swap in corsAllowAll() to
			// permit any origin instead.
			r.Use(corsRestricted())

			r.GET("/data", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})
			return nil
		}).
		MustRun()
}

// corsAllowAll permits any origin — convenient for public, credential-less APIs.
// DefaultCORSConfig sets AllowOrigins to ["*"] plus the common methods/headers.
func corsAllowAll() gin.HandlerFunc {
	return middleware.CORS(middleware.DefaultCORSConfig())
}

// corsRestricted permits only an explicit allowlist and enables credentials. The
// matching origin is reflected back; a wildcard origin is not allowed together
// with credentials (per the CORS spec), so the origins are listed explicitly.
func corsRestricted() gin.HandlerFunc {
	return middleware.CORS(middleware.CORSConfig{
		AllowOrigins:     []string{"https://app.example.com", "https://admin.example.com"},
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           1 * time.Hour,
	})
}
