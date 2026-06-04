package money

// defaultExponent is the minor-unit scale used for currencies not listed in the
// tables below. Most world currencies use two fractional digits (cents).
const defaultExponent int32 = 2

// zeroDecimal lists currencies with no minor unit, so an amount equals its
// minor-unit count (e.g. ¥100 is 100 minor units, not 10000).
var zeroDecimal = map[CurrencyCode]struct{}{
	"BIF": {}, "CLP": {}, "DJF": {}, "GNF": {}, "IRR": {}, "JPY": {},
	"KMF": {}, "KRW": {}, "PYG": {}, "RWF": {}, "UGX": {}, "VND": {},
	"VUV": {}, "XAF": {}, "XOF": {}, "XPF": {},
}

// threeDecimal lists currencies whose minor unit is one thousandth.
var threeDecimal = map[CurrencyCode]struct{}{
	"BHD": {}, "IQD": {}, "JOD": {}, "KWD": {}, "LYD": {}, "OMR": {}, "TND": {},
}

// Exponent returns the number of fractional digits in the currency's minor unit
// (its scale): 0 for zero-decimal currencies like IRR/JPY, 3 for currencies like
// KWD/BHD, and 2 for everything else. It is the bridge between a human-facing
// decimal amount and an integer minor-unit count (see FromMinorUnits / MinorUnits).
func (c CurrencyCode) Exponent() int32 {
	if _, ok := zeroDecimal[c]; ok {
		return 0
	}
	if _, ok := threeDecimal[c]; ok {
		return 3
	}
	return defaultExponent
}
