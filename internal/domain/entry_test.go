package domain

import (
	"math"
	"strings"
	"testing"
)

func TestNewLineValidation(t *testing.T) {
	_, err := NewLine("", Debit, money(t, 1, ars))
	wantErr(t, err, ErrInvalidID)
	_, err = NewLine("a", Side(0), money(t, 1, ars))
	wantErr(t, err, ErrInvalidAmount)
	_, err = NewLine("a", Debit, Money{})
	wantErr(t, err, ErrInvalidCurrency)
	for _, n := range []int64{0, -5} {
		_, err = NewLine("a", Credit, money(t, n, ars))
		wantErr(t, err, ErrInvalidAmount)
	}
	l := line(t, "a", Credit, 7, ars)
	if l.Account() != "a" || l.Side() != Credit || l.Amount().Minor() != 7 {
		t.Fatal("accessors")
	}
}

func TestJournalEntryMustBalancePerCurrency(t *testing.T) {
	e := entry(t, "e1", line(t, "cash", Debit, 100, ars), line(t, "equity", Credit, 60, ars), line(t, "equity", Credit, 40, ars))
	if e.ID() != "e1" || e.Memo() != "memo e1" || !e.PostedAt().Equal(t0) || e.ReversalOf() != "" || len(e.Lines()) != 3 {
		t.Fatal("accessors")
	}
	_, err := NewJournalEntry("e2", t0, "", line(t, "cash", Debit, 100, ars))
	wantErr(t, err, ErrTooFewLines)
	_, err = NewJournalEntry("e2", t0, "", line(t, "cash", Debit, 100, ars), line(t, "equity", Credit, 99, ars))
	wantErr(t, err, ErrUnbalanced)
	// Totals match numerically but not per currency.
	_, err = NewJournalEntry("e2", t0, "",
		line(t, "cash", Debit, 100, ars), line(t, "usd-equity", Credit, 100, usd))
	wantErr(t, err, ErrUnbalanced)
	// Multi-currency entry balanced in each currency is valid.
	if _, err = NewJournalEntry("e2", t0, "",
		line(t, "cash", Debit, 100, ars), line(t, "equity", Credit, 100, ars),
		line(t, "usd-cash", Debit, 3, usd), line(t, "usd-equity", Credit, 3, usd)); err != nil {
		t.Fatal(err)
	}
}

func TestJournalEntryRejectsBadInput(t *testing.T) {
	ok := []Line{line(t, "a", Debit, 1, ars), line(t, "b", Credit, 1, ars)}
	_, err := NewJournalEntry("", t0, "", ok...)
	wantErr(t, err, ErrInvalidID)
	var zero JournalEntry
	_, err = NewJournalEntry("e", zero.PostedAt(), "", ok...)
	wantErr(t, err, ErrInvalidAmount)
	_, err = NewJournalEntry("e", t0, strings.Repeat("m", 501), ok...)
	wantErr(t, err, ErrInvalidAmount)
	_, err = NewJournalEntry("e", t0, "", Line{}, Line{})
	wantErr(t, err, ErrInvalidAmount)
	_, err = NewJournalEntry("e", t0, "",
		line(t, "a", Debit, math.MaxInt64, ars), line(t, "a", Debit, 1, ars), line(t, "b", Credit, 1, ars))
	wantErr(t, err, ErrOverflow)
	_, err = NewJournalEntry("e", t0, "",
		line(t, "a", Debit, 1, ars), line(t, "b", Credit, math.MaxInt64, ars), line(t, "b", Credit, 1, ars))
	wantErr(t, err, ErrOverflow)
	_, err = NewReversalEntry("e", t0, "", "e", ok...)
	wantErr(t, err, ErrInvalidID)
	_, err = NewReversalEntry("e", t0, "", "bad id", ok...)
	wantErr(t, err, ErrInvalidID)
}

func TestJournalEntryIsImmutable(t *testing.T) {
	lines := []Line{line(t, "cash", Debit, 10, ars), line(t, "equity", Credit, 10, ars)}
	e := entry(t, "e1", lines...)
	lines[0] = line(t, "hacked", Debit, 999, ars)
	got := e.Lines()
	got[1] = line(t, "hacked", Credit, 999, ars)
	again := e.Lines()
	if again[0].Account() != "cash" || again[1].Account() != "equity" {
		t.Fatal("entry mutated through a slice alias")
	}
}

func TestEntryTimeIsNormalizedToUTC(t *testing.T) {
	loc := timeZone(t)
	e, err := NewJournalEntry("e", t0.In(loc), "", line(t, "a", Debit, 1, ars), line(t, "b", Credit, 1, ars))
	if err != nil {
		t.Fatal(err)
	}
	if e.PostedAt().Location().String() != "UTC" || !e.PostedAt().Equal(t0) {
		t.Fatal("not UTC")
	}
}
