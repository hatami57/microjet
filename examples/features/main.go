// Command features demonstrates microjet's HTTP feature set: the built-in
// request-id / metrics / readiness endpoints, plus opt-in CORS, rate limiting,
// JWT auth, a request-scoped logger, the cache module, and the JSON client with
// retries.
//
// Try it:
//
//	go run .
//	curl localhost:8080/greeting/world        # public; second call is cache hit
//	curl localhost:8080/healthz               # liveness (always /health)
//	curl localhost:8080/readyz                # readiness
//	curl localhost:8080/metrics               # prometheus metrics
//	curl localhost:8080/api/me                # 401 without a token
//
// A token for /api/me can be minted with any HS256 signer using "demo-secret".
package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hatami57/microjet/cache"
	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/host"
	"github.com/hatami57/microjet/httpx"
	"github.com/hatami57/microjet/httpx/middleware"
)

func main() {
	// WithCache registers the cache as a managed service: it loads the [cache]
	// section, connects at startup, and is closed on shutdown. Defaults to an
	// in-memory cache; set [cache] driver = "redis" to share entries across
	// replicas — no code change.
	app := host.MustNew(host.WithClock(core.UTC)).
		WithCache().
		WithHTTPServer(func(a *host.App) error {
			registerRoutes(a)
			return nil
		})

	app.MustRun()
}

func registerRoutes(a *host.App) {
	r := a.HTTPServer.Router
	// Setup handlers run after services are initialized, so the cache is ready.
	c := a.Cache()

	// Cross-origin + per-client rate limiting applied to every app route.
	// (RequestID, metrics, logging and error handling are installed by NewServer.)
	r.Use(middleware.CORS(middleware.DefaultCORSConfig()))
	r.Use(middleware.RateLimit(middleware.RateLimitConfig{RPS: 10, Burst: 20}))

	// Public route: uses the request-scoped logger (tagged with the request id)
	// and the cache.
	r.GET("/greeting/:name", func(ctx *gin.Context) {
		reqCtx := ctx.Request.Context()
		name := ctx.Param("name")
		log := httpx.LoggerFrom(reqCtx)
		key := "greeting:" + name

		if cached, found, _ := cache.GetJSON[string](reqCtx, c, key); found {
			log.Info("greeting cache hit", "name", name, "request_id", httpx.RequestID(ctx))
			ctx.JSON(http.StatusOK, gin.H{"greeting": cached, "cached": true})
			return
		}

		greeting := "Hello, " + name + "!"
		_ = cache.SetJSON(reqCtx, c, key, greeting, time.Minute)
		log.Info("greeting cache miss", "name", name, "request_id", httpx.RequestID(ctx))
		ctx.JSON(http.StatusOK, gin.H{"greeting": greeting, "cached": false})
	})

	// Outbound call demonstrating the JSON client with automatic retries on
	// transient failures, propagating the inbound request id upstream.
	r.GET("/proxy", func(ctx *gin.Context) {
		client := httpx.NewClient("https://httpbin.org", httpx.WithRetry(2, 200*time.Millisecond))
		var out map[string]any
		if err := client.GetJSON(ctx.Request.Context(), "/get", &out); err != nil {
			ctx.Error(err) // rendered by the error middleware
			return
		}
		ctx.JSON(http.StatusOK, out)
	})

	// Protected group: requires a valid HS256 JWT signed with "demo-secret".
	authed := r.Group("/api", middleware.JWT(middleware.JWTConfig{Secret: []byte("demo-secret")}))
	authed.GET("/me", func(ctx *gin.Context) {
		claims, _ := middleware.JWTClaimsFromContext(ctx.Request.Context())
		ctx.JSON(http.StatusOK, gin.H{"claims": claims})
	})
}
