package core

import (
	"strings"
	"time"
)

func TimeToSortable(t time.Time) string { return t.Format("20060102150405") }

// TimeToSortableMS formats t as a 17-digit lexicographically sortable string
// with millisecond precision: YYYYMMDDHHMMSSmmm. Go only recognizes fractional
// seconds when preceded by a separator, so we format with a dot and strip it.
func TimeToSortableMS(t time.Time) string {
	return strings.Replace(t.Format("20060102150405.000"), ".", "", 1)
}

func SortableToTime(st string) time.Time {
	t, _ := time.Parse("20060102150405", st)
	return t
}

func SortableMSToTime(st string) time.Time {
	if len(st) == 17 {
		st = st[:14] + "." + st[14:]
	}
	t, _ := time.Parse("20060102150405.000", st)
	return t
}

func TruncateToSecond(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, t.Location())
}
