package domain

import (
	"math"
	"testing"
)

func TestParseCurrency(t *testing.T) {
	for _, s := range []string{"ARS", "USD", "EUR"} {
		c, err := ParseCurrency(s)
		if err != nil || c.String() != s || c.IsZero() {
			t.Fatalf("%s: %v %v", s, c, err)
		}
	}
	for _, s := range []string{"", "ar", "ARSS", "ars", "A1S", "ÄRS"} {
		_, err := ParseCurrency(s)
		wantErr(t, err, ErrInvalidCurrency)
	}
}

func TestMustCurrencyPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	MustCurrency("x")
}

func TestMoneyArithmetic(t *testing.T) {
	a, b := money(t, 150, ars), money(t, 50, ars)
	s, err := a.Add(b)
	if err != nil || s.Minor() != 200 || s.Currency() != ars || s.String() != "200 ARS" {
		t.Fatalf("add: %v %v", s, err)
	}
	d, err := b.Sub(a)
	if err != nil || d.Minor() != -100 || d.IsPositive() {
		t.Fatalf("sub: %v %v", d, err)
	}
	_, err = a.Add(money(t, 1, usd))
	wantErr(t, err, ErrCurrencyMismatch)
	_, err = a.Sub(money(t, 1, usd))
	wantErr(t, err, ErrCurrencyMismatch)
	_, err = NewMoney(1, Currency{})
	wantErr(t, err, ErrInvalidCurrency)
}

func TestMoneyOverflowIsAnError(t *testing.T) {
	max, one := money(t, math.MaxInt64, ars), money(t, 1, ars)
	_, err := max.Add(one)
	wantErr(t, err, ErrOverflow)
	min := money(t, math.MinInt64, ars)
	_, err = min.Add(money(t, -1, ars))
	wantErr(t, err, ErrOverflow)
	_, err = min.Neg()
	wantErr(t, err, ErrOverflow)
	_, err = one.Sub(min)
	wantErr(t, err, ErrOverflow)
}
