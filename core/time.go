package core

import (
	"strings"
	"time"
)

type TimeProvider interface {
	Now() time.Time
	NowTS() int64
	NowSortable() string
	NowSortableMS() string
}

type SystemClock struct{}

var Clock SystemClock

func (c *SystemClock) Now() time.Time        { return time.Now().UTC() }
func (c *SystemClock) NowTS() int64          { return c.Now().Unix() }
func (c *SystemClock) NowSortable() string   { return TimeToSortable(c.Now()) }
func (c *SystemClock) NowSortableMS() string { return TimeToSortableMS(c.Now()) }

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
