package money

import (
	"errors"

	"github.com/hatami57/microjet/utils"
	"github.com/shopspring/decimal"
)

var ErrDifferentCurrency = errors.New("different currency")

type CurrencyCode string

type Money struct {
	Value        decimal.Decimal `json:"value"`
	CurrencyCode CurrencyCode    `json:"currencyCode"`
}

var Zero = Money{Value: decimal.Zero, CurrencyCode: ""}

func FromMap(m map[string]interface{}) (*Money, error) {
	return utils.MapTo[*Money](m)
}

func (m *Money) ToMap() map[string]interface{} {
	if m == nil {
		return nil
	}
	mm, _ := utils.ToMap(m)
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
