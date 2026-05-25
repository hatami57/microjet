package money

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func usd(v float64) Money {
	return Money{Value: decimal.NewFromFloat(v), CurrencyCode: "USD"}
}

func TestAddSameCurrency(t *testing.T) {
	a, b := usd(10.50), usd(21)
	sum, err := a.Add(&b)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !sum.Value.Equal(decimal.NewFromFloat(31.50)) || sum.CurrencyCode != "USD" {
		t.Fatalf("sum = %+v, want 31.50 USD", sum)
	}
}

func TestArithmeticDifferentCurrencyFails(t *testing.T) {
	a := usd(10)
	eur := Money{Value: decimal.NewFromInt(10), CurrencyCode: "EUR"}

	if _, err := a.Add(&eur); !errors.Is(err, ErrDifferentCurrency) {
		t.Errorf("Add cross-currency err = %v, want ErrDifferentCurrency", err)
	}
	if _, err := a.Sub(&eur); !errors.Is(err, ErrDifferentCurrency) {
		t.Errorf("Sub cross-currency err = %v, want ErrDifferentCurrency", err)
	}
	if _, err := a.Multiply(&eur); !errors.Is(err, ErrDifferentCurrency) {
		t.Errorf("Multiply cross-currency err = %v, want ErrDifferentCurrency", err)
	}
}

func TestMultiplyInt64(t *testing.T) {
	price := usd(10.50)
	got := price.MultiplyInt64(2)
	if !got.Value.Equal(decimal.NewFromInt(21)) {
		t.Errorf("MultiplyInt64 = %v, want 21", got.Value)
	}
}

func TestEqualAndIsZero(t *testing.T) {
	a, b := usd(5), usd(5)
	if !a.Equal(&b) {
		t.Error("expected equal amounts to compare equal")
	}
	if !Zero.IsZero() {
		t.Error("Zero.IsZero() = false")
	}
	if a.IsZero() {
		t.Error("non-zero amount reported as zero")
	}
}
