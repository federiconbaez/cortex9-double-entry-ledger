package domain

import (
	"fmt"
	"math"
)

// Currency is an ISO-4217-shaped code: exactly three ASCII letters A-Z.
// The zero value is invalid and rejected by every constructor.
type Currency struct{ code string }

// ParseCurrency validates and returns a Currency.
func ParseCurrency(s string) (Currency, error) {
	if len(s) != 3 {
		return Currency{}, errf(KindInvalidCurrency, "%q must be 3 letters", s)
	}
	for i := 0; i < 3; i++ {
		if s[i] < 'A' || s[i] > 'Z' {
			return Currency{}, errf(KindInvalidCurrency, "%q must be uppercase A-Z", s)
		}
	}
	return Currency{code: s}, nil
}

// MustCurrency is ParseCurrency for constants in tests and examples; it panics on error.
func MustCurrency(s string) Currency {
	c, err := ParseCurrency(s)
	if err != nil {
		panic(err)
	}
	return c
}

func (c Currency) String() string { return c.code }

// IsZero reports whether c is the invalid zero Currency.
func (c Currency) IsZero() bool { return c.code == "" }

// Money is an amount in integer minor units (e.g. cents) of one currency.
// There is deliberately no float anywhere in its API.
type Money struct {
	minor int64
	cur   Currency
}

// NewMoney builds a Money; the currency must be valid.
func NewMoney(minor int64, cur Currency) (Money, error) {
	if cur.IsZero() {
		return Money{}, errf(KindInvalidCurrency, "zero currency")
	}
	return Money{minor: minor, cur: cur}, nil
}

// Minor returns the amount in minor units.
func (m Money) Minor() int64 { return m.minor }

// Currency returns the currency.
func (m Money) Currency() Currency { return m.cur }

// IsPositive reports whether the amount is > 0.
func (m Money) IsPositive() bool { return m.minor > 0 }

func (m Money) String() string { return fmt.Sprintf("%d %s", m.minor, m.cur.code) }

// Add returns m+o. Different currencies or int64 overflow are errors.
func (m Money) Add(o Money) (Money, error) {
	if m.cur != o.cur {
		return Money{}, errf(KindCurrencyMismatch, "%s + %s", m.cur.code, o.cur.code)
	}
	s, ok := addInt64(m.minor, o.minor)
	if !ok {
		return Money{}, errf(KindOverflow, "%s + %s", m, o)
	}
	return Money{minor: s, cur: m.cur}, nil
}

// Sub returns m-o. Different currencies or int64 overflow are errors.
func (m Money) Sub(o Money) (Money, error) {
	n, err := o.Neg()
	if err != nil {
		return Money{}, err
	}
	return m.Add(n)
}

// Neg returns -m; negating math.MinInt64 overflows.
func (m Money) Neg() (Money, error) {
	if m.minor == math.MinInt64 {
		return Money{}, errf(KindOverflow, "-(%s)", m)
	}
	return Money{minor: -m.minor, cur: m.cur}, nil
}

func addInt64(a, b int64) (int64, bool) {
	s := a + b
	if (b > 0 && s < a) || (b < 0 && s > a) {
		return 0, false
	}
	return s, true
}
