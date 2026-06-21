// Command time demonstrates MicroJet's time utilities (core): the TimeProvider
// interface that makes "now" injectable (and therefore testable), the real UTC
// clock, the FixedClock for tests, and the sortable timestamp encodings used for
// lexically orderable keys.
//
// Run it with:
//
//	go run .
package main

import (
	"fmt"
	"time"

	"github.com/hatami57/microjet/core"
)

// report depends on a TimeProvider rather than calling time.Now() directly, so
// the caller decides what "now" is. Production passes core.UTC; tests pass a
// FixedClock. This is the whole point of the interface.
func report(clock core.TimeProvider) string {
	return "generated at " + clock.NowSortable()
}

func main() {
	// 1. The real clock. core.UTC is the default TimeProvider used across the
	// framework when no clock is injected; it always returns UTC time.
	fmt.Println("== real clock (core.UTC) ==")
	fmt.Printf("  Now()         = %s\n", core.UTC.Now().Format(time.RFC3339))
	fmt.Printf("  NowTS()       = %d (unix seconds)\n", core.UTC.NowTS())
	fmt.Printf("  NowSortable() = %s\n", core.UTC.NowSortable())

	// 2. A deterministic clock for tests. FixedClock implements TimeProvider, so
	// it is a drop-in substitute; Advance moves time forward without sleeping.
	fixed := core.NewFixedClock(time.Date(2026, 6, 21, 10, 30, 0, 0, time.UTC))
	fmt.Println("\n== fixed clock (tests) ==")
	fmt.Printf("  report(fixed)     = %s\n", report(fixed))
	fixed.Advance(48 * time.Hour)
	fmt.Printf("  after +48h        = %s\n", report(fixed))

	// 3. Sortable encodings. These format a timestamp so string comparison equals
	// time comparison — handy for cursor keys, object names, and log prefixes.
	t := time.Date(2026, 6, 21, 10, 30, 15, 123_000_000, time.UTC)
	sortable := core.TimeToSortable(t)
	sortableMS := core.TimeToSortableMS(t)
	fmt.Println("\n== sortable timestamps ==")
	fmt.Printf("  TimeToSortable    = %s\n", sortable)
	fmt.Printf("  TimeToSortableMS  = %s\n", sortableMS)
	fmt.Printf("  round-trips back  = %s\n", core.SortableToTime(sortable).Format(time.RFC3339))
}
