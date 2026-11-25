package domain

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// snapshot renders the full observable state of a ledger deterministically.
func snapshot(t testing.TB, l *Ledger) string {
	t.Helper()
	var b strings.Builder
	for _, a := range l.Accounts() {
		s, err := l.SignedBalance(a.ID())
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "%s|%s|%s|%v|%d\n", a.ID(), a.Type(), a.Currency(), a.Policy(), s.Minor())
	}
	ids := make([]string, 0, len(l.entries))
	for id := range l.entries {
		ids = append(ids, string(id))
	}
	slices.Sort(ids)
	for _, id := range ids {
		e := l.entries[EntryID(id)]
		fmt.Fprintf(&b, "E %s rev=%s at=%s lines=%d\n", id, e.ReversalOf(), e.PostedAt().Format("2006-01-02T15:04:05.999999999Z"), len(e.lines))
	}
	return b.String()
}

func openingEvents(t testing.TB) []Event {
	t.Helper()
	l := NewLedger()
	lim, _ := OverdraftLimit(500)
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
	return l.Pending()
}
