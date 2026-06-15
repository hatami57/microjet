package money

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestExponent(t *testing.T) {
	cases := map[CurrencyCode]int32{
		"USD": 2, "EUR": 2,
		"IRR": 0, "JPY": 0, "KRW": 0,
		"KWD": 3, "BHD": 3,
		"XYZ": 2, // unknown → default
	}
	for c, want := range cases {
		if got := c.Exponent(); got != want {
			t.Errorf("%s.Exponent() = %d, want %d", c, got, want)
		}
	}
}

func TestFromMinorUnits(t *testing.T) {
	cases := []struct {
		amount   int64
		currency CurrencyCode
		want     string
	}{
		{1050, "USD", "10.5"},
		{1050, "IRR", "1050"},
		{1050, "KWD", "1.05"},
		{0, "USD", "0"},
	}
	for _, c := range cases {
		got := FromMinorUnits(c.amount, c.currency)
		if got.CurrencyCode != c.currency {
			t.Errorf("FromMinorUnits(%d,%s) currency = %s", c.amount, c.currency, got.CurrencyCode)
		}
		if !got.Value.Equal(decimal.RequireFromString(c.want)) {
			t.Errorf("FromMinorUnits(%d,%s) = %s, want %s", c.amount, c.currency, got.Value, c.want)
		}
	}
}

func TestMinorUnitsRoundTrip(t *testing.T) {
	for _, c := range []CurrencyCode{"USD", "IRR", "KWD"} {
		m := FromMinorUnits(123456, c)
		if got := m.MinorUnits(); got != 123456 {
			t.Errorf("round trip %s: MinorUnits() = %d, want 123456", c, got)
		}
	}
}

func TestMinorUnitsRounds(t *testing.T) {
	m := Money{Value: decimal.RequireFromString("10.509"), CurrencyCode: "USD"}
	if got := m.MinorUnits(); got != 1051 {
		t.Errorf("MinorUnits() = %d, want 1051 (rounded)", got)
	}
}
