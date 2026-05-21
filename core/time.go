package core

import "time"

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

func TimeToSortableMS(t time.Time) string { return t.Format("20060102150405000") }
func SortableMSToTime(st string) time.Time {
	t, _ := time.Parse("20060102150405000", st)
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
