package domain

import (
	"errors"
	"testing"
	"time"
)

var (
	ars = MustCurrency("ARS")
	usd = MustCurrency("USD")
	t0  = time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
)

func money(t testing.TB, minor int64, c Currency) Money {
	t.Helper()
	m, err := NewMoney(minor, c)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func line(t testing.TB, acct string, side Side, minor int64, c Currency) Line {
	t.Helper()
	l, err := NewLine(AccountID(acct), side, money(t, minor, c))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func entry(t testing.TB, id string, lines ...Line) JournalEntry {
	t.Helper()
	e, err := NewJournalEntry(EntryID(id), t0, "memo "+id, lines...)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func account(t testing.TB, id string, typ AccountType, c Currency, p OverdraftPolicy) Account {
	t.Helper()
	a, err := NewAccount(AccountID(id), "acct "+id, typ, c, p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func wantErr(t testing.TB, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("err = %v, want %v", err, target)
	}
}

// bank returns a ledger with: cash (asset, unrestricted), wallet
// (liability, no overdraft), credit (liability, limit 500), equity, usd cash.
func bank(t testing.TB) *Ledger {
	t.Helper()
	l := NewLedger()
	lim, err := OverdraftLimit(500)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []Account{
		account(t, "cash", Asset, ars, Unrestricted()),
		account(t, "wallet", Liability, ars, NoOverdraft()),
		account(t, "credit", Liability, ars, lim),
		account(t, "equity", Equity, ars, Unrestricted()),
		account(t, "usd-cash", Asset, usd, Unrestricted()),
		account(t, "usd-equity", Equity, usd, Unrestricted()),
	} {
		if err := l.OpenAccount(a, t0); err != nil {
			t.Fatal(err)
		}
	}
	l.MarkCommitted()
	return l
}
