package money

import (
	"errors"

	"github.com/hatami57/microjet/jsonx"
	"github.com/shopspring/decimal"
)

var ErrDifferentCurrency = errors.New("different currency")

type CurrencyCode string

type Money struct {
	Value        decimal.Decimal `json:"value"`
	CurrencyCode CurrencyCode    `json:"currencyCode"`
}

var Zero = Money{Value: decimal.Zero, CurrencyCode: ""}

func FromMap(m map[string]any) (*Money, error) {
	return jsonx.MapTo[*Money](m)
}

func (m *Money) ToMap() map[string]any {
	if m == nil {
		return nil
	}
	mm, _ := jsonx.ToMap(m)
	return mm
}

func (m *Money) Add(money *Money) (Money, error) {
	if m.CurrencyCode != money.CurrencyCode {
		return Zero, ErrDifferentCurrency
	}
	return Money{Value: m.Value.Add(money.Value), CurrencyCode: m.CurrencyCode}, nil
}

func (m *Money) Sub(money *Money) (Money, error) {
	if m.CurrencyCode != money.CurrencyCode {
		return Zero, ErrDifferentCurrency
	}
	return Money{Value: m.Value.Sub(money.Value), CurrencyCode: m.CurrencyCode}, nil
}

func (m *Money) Multiply(money *Money) (Money, error) {
	if m.CurrencyCode != money.CurrencyCode {
		return Zero, ErrDifferentCurrency
	}
	return Money{Value: m.Value.Mul(money.Value), CurrencyCode: m.CurrencyCode}, nil
}

func (m *Money) MultiplyValue(value decimal.Decimal) Money {
	return Money{Value: m.Value.Mul(value), CurrencyCode: m.CurrencyCode}
}

func (m *Money) MultiplyInt64(value int64) Money {
	return Money{Value: m.Value.Mul(decimal.NewFromInt(value)), CurrencyCode: m.CurrencyCode}
}

func (m *Money) Equal(money *Money) bool {
	return m.CurrencyCode == money.CurrencyCode && m.Value.Equal(money.Value)
}

func (m *Money) IsZero() bool { return m.Value.Equal(decimal.Zero) }

// FromMinorUnits builds a Money from an integer count of the currency's smallest
// unit — cents for USD, fils for KWD, whole Rials for IRR — using the currency's
// Exponent. For example FromMinorUnits(1050, "USD") is 10.50 USD, while
// FromMinorUnits(1050, "IRR") is 1050 IRR (IRR has no minor unit). Use it when a
// payment gateway or ledger speaks integer amounts.
func FromMinorUnits(amount int64, currency CurrencyCode) Money {
	return Money{Value: decimal.New(amount, -currency.Exponent()), CurrencyCode: currency}
}

// MinorUnits returns the amount as an integer count of the currency's smallest
// unit (the inverse of FromMinorUnits). The value is scaled by the currency's
// Exponent and rounded to the nearest whole minor unit, so 10.50 USD yields 1050
// and 1050 IRR yields 1050.
func (m *Money) MinorUnits() int64 {
	return m.Value.Shift(m.CurrencyCode.Exponent()).Round(0).IntPart()
}
