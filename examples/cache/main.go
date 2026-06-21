// Command cache demonstrates MicroJet's cache abstraction (cache.Cache): the
// typed GetJSON/SetJSON helpers, raw bytes, deletion, and TTL expiry. It uses
// the in-memory implementation directly so it runs offline and is fully
// deterministic — TTL is driven by an injected clock, so we expire entries by
// advancing time instead of sleeping.
//
// In a real service you would not construct the cache by hand: add
// cache.Module() to the host and reach it with cache.Of(app). The Module loads
// the [cache] section, so switching driver = "redis" shares entries across
// replicas with no code change. The Cache interface below is identical either way.
//
// Run it with:
//
//	go run .
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/hatami57/microjet/cache"
	"github.com/hatami57/microjet/core"
)

type Session struct {
	UserID int    `json:"userID"`
	Role   string `json:"role"`
}

func main() {
	ctx := context.Background()

	// A FixedClock lets us demonstrate TTL expiry without real sleeps: the cache
	// reads "now" from this clock, so Advance makes entries expire on command.
	clock := core.NewFixedClock(time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC))
	c := cache.NewMemoryCache(clock)

	// 1. Typed values via the generic helpers — JSON-encoded under the hood.
	_ = cache.SetJSON(ctx, c, "session:42", Session{UserID: 42, Role: "admin"}, time.Minute)
	if s, found, _ := cache.GetJSON[Session](ctx, c, "session:42"); found {
		fmt.Printf("== typed get == %+v\n", s)
	}

	// 2. A miss returns found=false rather than an error.
	if _, found, _ := cache.GetJSON[Session](ctx, c, "session:99"); !found {
		fmt.Println("== miss == session:99 not cached")
	}

	// 3. Delete removes an entry immediately.
	_ = c.SetBytes(ctx, "token", []byte("abc123"), time.Hour)
	_ = c.Delete(ctx, "token")
	if _, found, _ := c.GetBytes(ctx, "token"); !found {
		fmt.Println("== delete == token removed")
	}

	// 4. TTL expiry. The entry is live now; after we advance past its TTL the
	// same key reports a miss and is evicted.
	_ = cache.SetJSON(ctx, c, "otp", "999111", 30*time.Second)
	_, liveBefore, _ := cache.GetJSON[string](ctx, c, "otp")
	clock.Advance(31 * time.Second)
	_, liveAfter, _ := cache.GetJSON[string](ctx, c, "otp")
	fmt.Printf("== ttl == before advance: found=%v, after +31s: found=%v\n", liveBefore, liveAfter)
}
