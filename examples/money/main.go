// Command money demonstrates MicroJet's currency-aware Money type
// (core/types/money): construction, safe arithmetic that refuses to mix
// currencies, and integer minor-unit conversion driven by a currency exponent
// registry (JPY has 0 decimals, KWD has 3, USD has 2).
//
// Run it with:
//
//	go run .
package main

import (
	"fmt"

	"github.com/hatami57/microjet/core/types/money"
)

func main() {
	// 1. Construction. Build from a string (exact), an int (whole units), a float
	// (convenience), or raw minor units. The currency code travels with the value.
	price, _ := money.NewFromString("19.99", "USD")
	tax := money.NewFromFloat(1.60, "USD")
	fmt.Println("== construction ==")
	fmt.Printf("  price = %s %s\n", price.Value, price.CurrencyCode)
	fmt.Printf("  tax   = %s %s\n", tax.Value, tax.CurrencyCode)

	// 2. Arithmetic. Add/Sub/Multiply return an error if the currencies differ,
	// so you can never silently add dollars to euros. Multiply takes another
	// Money (treated as a scalar); MultiplyInt64 is the common quantity case.
	total, err := price.Add(&tax)
	if err != nil {
		panic(err)
	}
	lineItem := price.MultiplyInt64(3)
	fmt.Println("\n== arithmetic ==")
	fmt.Printf("  price + tax       = %s %s\n", total.Value, total.CurrencyCode)
	fmt.Printf("  price * 3 (qty)   = %s %s\n", lineItem.Value, lineItem.CurrencyCode)

	// 3. Mixing currencies is a hard error, not a wrong number.
	euros := money.NewFromInt(5, "EUR")
	if _, err := price.Add(&euros); err != nil {
		fmt.Println("\n== currency mismatch ==")
		fmt.Printf("  USD + EUR -> %v\n", err)
	}

	// 4. Minor units. The number of fractional digits comes from the currency's
	// exponent, so the same int64 means different things per currency. This is
	// the integer form to store in a database or send over the wire.
	fmt.Println("\n== minor units (exponent registry) ==")
	for _, m := range []money.Money{
		money.NewFromInt(10, "USD"), // 2 decimals -> 1000 cents
		money.NewFromInt(10, "JPY"), // 0 decimals -> 10 yen
		money.NewFromInt(10, "KWD"), // 3 decimals -> 10000 fils
	} {
		fmt.Printf("  %s 10.00 -> exponent %d -> %d minor units\n",
			m.CurrencyCode, m.CurrencyCode.Exponent(), m.MinorUnits())
	}

	// 5. Round-trip from minor units back to a decimal amount.
	restored := money.FromMinorUnits(2599, "USD")
	fmt.Println("\n== from minor units ==")
	fmt.Printf("  2599 USD minor units -> %s\n", restored.Value)
}
