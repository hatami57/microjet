package core

import (
	"testing"
	"time"
)

func TestSortableRoundTrip(t *testing.T) {
	original := time.Date(2026, 5, 22, 13, 45, 30, 0, time.UTC)
	got := SortableToTime(TimeToSortable(original))
	if !got.Equal(original) {
		t.Errorf("round trip = %v, want %v", got, original)
	}
}

func TestSortableMSRoundTrip(t *testing.T) {
	original := time.Date(2026, 5, 22, 13, 45, 30, 123000000, time.UTC)
	got := SortableMSToTime(TimeToSortableMS(original))
	if !got.Equal(original) {
		t.Errorf("round trip = %v, want %v", got, original)
	}
}

func TestTruncateToSecond(t *testing.T) {
	in := time.Date(2026, 5, 22, 13, 45, 30, 999, time.UTC)
	got := TruncateToSecond(in)
	if got.Nanosecond() != 0 {
		t.Errorf("nanoseconds not truncated: %d", got.Nanosecond())
	}
}

func TestSystemClockIsUTC(t *testing.T) {
	c := &SystemClock{}
	if loc := c.Now().Location(); loc != time.UTC {
		t.Errorf("Now location = %v, want UTC", loc)
	}
}
