package core

import (
	"strings"
	"time"
)

// TimeProvider supplies the current time. Inject it (e.g. via host.WithClock)
// so time-dependent code can be made deterministic in tests by swapping in a
// FixedClock instead of reaching for time.Now() directly.
type TimeProvider interface {
	Now() time.Time
	NowTS() int64
	NowSortable() string
	NowSortableMS() string
}

// SystemClock is a TimeProvider backed by the real wall clock, normalized to UTC.
type SystemClock struct{}

// UTC is the default real-time clock, used when no clock is injected.
var UTC TimeProvider = SystemClock{}

// Clock is a process-wide SystemClock kept for backward compatibility. Prefer
// injecting a TimeProvider (host.WithClock) over reaching for this global.
var Clock = SystemClock{}

// Value receivers so both SystemClock{} and &SystemClock{} satisfy TimeProvider.
func (SystemClock) Now() time.Time          { return time.Now().UTC() }
func (c SystemClock) NowTS() int64          { return c.Now().Unix() }
func (c SystemClock) NowSortable() string   { return TimeToSortable(c.Now()) }
func (c SystemClock) NowSortableMS() string { return TimeToSortableMS(c.Now()) }

// FixedClock is a TimeProvider that reports a preset time, for deterministic
// tests. It is not safe for concurrent mutation; set the time before use.
type FixedClock struct{ T time.Time }

// NewFixedClock returns a FixedClock pinned to t (normalized to UTC).
func NewFixedClock(t time.Time) *FixedClock { return &FixedClock{T: t.UTC()} }

// Set replaces the time the clock reports.
func (c *FixedClock) Set(t time.Time) { c.T = t.UTC() }

// Advance moves the clock forward by d.
func (c *FixedClock) Advance(d time.Duration) { c.T = c.T.Add(d) }

func (c *FixedClock) Now() time.Time        { return c.T }
func (c *FixedClock) NowTS() int64          { return c.T.Unix() }
func (c *FixedClock) NowSortable() string   { return TimeToSortable(c.T) }
func (c *FixedClock) NowSortableMS() string { return TimeToSortableMS(c.T) }

// TimeToSortableMS formats t as a 17-digit lexicographically sortable string
// with millisecond precision: YYYYMMDDHHMMSSmmm. Go only recognizes fractional
// seconds when preceded by a separator, so we format with a dot and strip it.
func TimeToSortableMS(t time.Time) string {
	return strings.Replace(t.Format("20060102150405.000"), ".", "", 1)
}

func SortableMSToTime(st string) time.Time {
	if len(st) == 17 {
		st = st[:14] + "." + st[14:]
	}
	t, _ := time.Parse("20060102150405.000", st)
	return t
}

func TimeToSortable(t time.Time) string { return t.Format("20060102150405") }
func SortableToTime(st string) time.Time {
	t, _ := time.Parse("20060102150405", st)
	return t
}

func TruncateToSecond(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, t.Location())
}
