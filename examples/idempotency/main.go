// Command idempotency demonstrates the Idempotency-Key middleware
// (httpx/middleware.Idempotency): a client that retries a non-safe request with
// the same key gets the original stored response replayed instead of the
// handler running twice. Keys are scoped by method+route and stored in any
// IdempotencyStore — the app cache (cache.Of) satisfies that interface directly.
//
// Run it, then replay a request:
//
//	go run .
//	# first call runs the handler and returns a fresh charge id:
//	curl -s -XPOST localhost:8080/charges -H 'Idempotency-Key: abc' -d '{"amount":100}'
//	# same key -> identical response, handler NOT re-run (note Idempotent-Replayed):
//	curl -si -XPOST localhost:8080/charges -H 'Idempotency-Key: abc' -d '{"amount":100}'
//	# different key -> a new charge:
//	curl -s -XPOST localhost:8080/charges -H 'Idempotency-Key: xyz' -d '{"amount":100}'
package main

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hatami57/microjet/cache"
	"github.com/hatami57/microjet/host"
	"github.com/hatami57/microjet/httpx"
	"github.com/hatami57/microjet/httpx/middleware"
)

// chargeCounter stands in for a side effect (charging a card). We can tell the
// handler ran by whether this advances — a replayed response must NOT advance it.
var chargeCounter atomic.Int64

func main() {
	host.MustNew().
		WithModule(cache.Module()).
		WithModule(httpx.Module()).
		Setup(func(a *host.App) error {
			r := httpx.Of(a).Router

			// The app cache is an IdempotencyStore as-is. Replay POST/PUT/PATCH/
			// DELETE for 10 minutes per key.
			r.POST("/charges",
				middleware.Idempotency(cache.Of(a), middleware.WithIdempotencyTTL(10*time.Minute)),
				func(c *gin.Context) {
					id := chargeCounter.Add(1) // the "side effect" we must not repeat
					c.JSON(http.StatusCreated, gin.H{
						"chargeID": id,
						"status":   "captured",
					})
				})
			return nil
		}).
		MustRun()
}
