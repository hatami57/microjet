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

func TestUTCAndClockSatisfyTimeProvider(t *testing.T) {
	// Both the package default and value/pointer forms implement TimeProvider.
	var _ TimeProvider = UTC
	var _ TimeProvider = SystemClock{}
	var _ TimeProvider = &SystemClock{}
	if UTC.Now().Location() != time.UTC {
		t.Errorf("UTC.Now not in UTC")
	}
}

func TestFixedClock(t *testing.T) {
	base := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	c := NewFixedClock(base)

	var _ TimeProvider = c // implements the interface

	if !c.Now().Equal(base) {
		t.Errorf("Now = %v, want %v", c.Now(), base)
	}
	if c.NowTS() != base.Unix() {
		t.Errorf("NowTS = %d, want %d", c.NowTS(), base.Unix())
	}
	if got, want := c.NowSortable(), TimeToSortable(base); got != want {
		t.Errorf("NowSortable = %q, want %q", got, want)
	}

	c.Advance(90 * time.Second)
	if !c.Now().Equal(base.Add(90 * time.Second)) {
		t.Errorf("after Advance Now = %v, want %v", c.Now(), base.Add(90*time.Second))
	}

	c.Set(base)
	if !c.Now().Equal(base) {
		t.Errorf("after Set Now = %v, want %v", c.Now(), base)
	}
}

func TestFixedClockNormalizesToUTC(t *testing.T) {
	loc := time.FixedZone("X", 3600)
	c := NewFixedClock(time.Date(2026, 6, 5, 12, 0, 0, 0, loc))
	if c.Now().Location() != time.UTC {
		t.Errorf("FixedClock location = %v, want UTC", c.Now().Location())
	}
}
