package domain

import (
	"math"
	"slices"
	"testing"
)

func mustBalance(t *testing.T, l *Ledger, id string, want int64) {
	t.Helper()
	b, err := l.Balance(AccountID(id))
	if err != nil {
		t.Fatal(err)
	}
	if b.Minor() != want {
		t.Fatalf("balance %s = %d, want %d", id, b.Minor(), want)
	}
}

func TestOpenAccount(t *testing.T) {
	l := bank(t)
	if l.Version() != 6 || len(l.Accounts()) != 6 || l.Accounts()[0].ID() != "cash" {
		t.Fatalf("version %d accounts %d", l.Version(), len(l.Accounts()))
	}
	err := l.OpenAccount(account(t, "cash", Asset, ars, Unrestricted()), t0)
	wantErr(t, err, ErrDuplicateAccount)
	err = l.OpenAccount(Account{}, t0)
	wantErr(t, err, ErrInvalidID)
	if len(l.Pending()) != 0 {
		t.Fatal("rejected command left pending events")
	}
	if _, ok := l.Account("wallet"); !ok {
		t.Fatal("wallet missing")
	}
}

func TestPostDerivesBalancesFromEvents(t *testing.T) {
	l := bank(t)
	// Customer deposits 1000: bank cash up (debit), wallet liability up (credit).
	if err := l.Post(entry(t, "dep", line(t, "cash", Debit, 1000, ars), line(t, "wallet", Credit, 1000, ars))); err != nil {
		t.Fatal(err)
	}
	mustBalance(t, l, "cash", 1000)
	mustBalance(t, l, "wallet", 1000)
	s, _ := l.SignedBalance("wallet")
	if s.Minor() != -1000 {
		t.Fatalf("signed wallet = %d", s.Minor())
	}
	if len(l.Pending()) != 1 || l.Pending()[0].EventType() != TypeEntryPosted {
		t.Fatal("pending event")
	}
	l.MarkCommitted()
	if l.Version() != 7 || l.EntryCount() != 1 {
		t.Fatal("version/entries")
	}
	if e, ok := l.Entry("dep"); !ok || e.ID() != "dep" {
		t.Fatal("entry lookup")
	}
	tb, err := l.TrialBalance()
	if err != nil || tb[ars] != 0 || tb[usd] != 0 {
		t.Fatalf("trial balance %v %v", tb, err)
	}
}

func TestPostRejections(t *testing.T) {
	l := bank(t)
	ok := entry(t, "dep", line(t, "cash", Debit, 10, ars), line(t, "wallet", Credit, 10, ars))
	if err := l.Post(ok); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		e    JournalEntry
		want error
	}{
		{"duplicate id", ok, ErrDuplicateEntry},
		{"unknown account", entry(t, "x1", line(t, "cash", Debit, 1, ars), line(t, "ghost", Credit, 1, ars)), ErrUnknownAccount},
		{"currency of account", entry(t, "x2", line(t, "usd-cash", Debit, 1, ars), line(t, "equity", Credit, 1, ars)), ErrCurrencyMismatch},
		{"no overdraft", entry(t, "x3", line(t, "wallet", Debit, 11, ars), line(t, "cash", Credit, 11, ars)), ErrInsufficientFunds},
		{"over limit", entry(t, "x4", line(t, "credit", Debit, 501, ars), line(t, "cash", Credit, 501, ars)), ErrInsufficientFunds},
		{"zero value", JournalEntry{}, ErrInvalidID},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := snapshot(t, l)
			wantErr(t, l.Post(c.e), c.want)
			if after := snapshot(t, l); after != before {
				t.Fatalf("state changed on rejection:\n%s\n%s", before, after)
			}
		})
	}
}

func TestOverdraftLimitBoundaryAndRepayment(t *testing.T) {
	l := bank(t)
	if err := l.Post(entry(t, "draw", line(t, "credit", Debit, 500, ars), line(t, "cash", Credit, 500, ars))); err != nil {
		t.Fatal(err)
	}
	mustBalance(t, l, "credit", -500)
	// Repaying part of the overdraft is always allowed (non-worsening).
	if err := l.Post(entry(t, "repay", line(t, "cash", Debit, 100, ars), line(t, "credit", Credit, 100, ars))); err != nil {
		t.Fatal(err)
	}
	mustBalance(t, l, "credit", -400)
}

func TestReverse(t *testing.T) {
	l := bank(t)
	if err := l.Post(entry(t, "dep", line(t, "cash", Debit, 300, ars), line(t, "wallet", Credit, 300, ars))); err != nil {
		t.Fatal(err)
	}
	if err := l.Reverse("dep", "dep-rev", t0, "chargeback"); err != nil {
		t.Fatal(err)
	}
	mustBalance(t, l, "cash", 0)
	mustBalance(t, l, "wallet", 0)
	rev, _ := l.Entry("dep-rev")
	if rev.ReversalOf() != "dep" || rev.Lines()[0].Side() != Credit {
		t.Fatal("reversal shape")
	}
	wantErr(t, l.Reverse("dep", "again", t0, ""), ErrAlreadyReversed)
	wantErr(t, l.Reverse("dep-rev", "rev-rev", t0, ""), ErrAlreadyReversed)
	wantErr(t, l.Reverse("ghost", "r", t0, ""), ErrUnknownEntry)
	wantErr(t, l.Reverse("dep", "", t0, ""), ErrInvalidID)
	// Post refuses to smuggle a reversal.
	smuggled, err := NewReversalEntry("s", t0, "", "dep", line(t, "cash", Credit, 1, ars), line(t, "wallet", Debit, 1, ars))
	if err != nil {
		t.Fatal(err)
	}
	wantErr(t, l.Post(smuggled), ErrInvalidID)
}

func TestReverseCanBeBlockedByPolicy(t *testing.T) {
	l := bank(t)
	if err := l.Post(entry(t, "dep", line(t, "cash", Debit, 300, ars), line(t, "wallet", Credit, 300, ars))); err != nil {
		t.Fatal(err)
	}
	if err := l.Post(entry(t, "spend", line(t, "wallet", Debit, 300, ars), line(t, "cash", Credit, 300, ars))); err != nil {
		t.Fatal(err)
	}
	// Reversing the deposit would leave the wallet at -300: refused.
	wantErr(t, l.Reverse("dep", "dep-rev", t0, ""), ErrInsufficientFunds)
}

func TestReplayRebuildsIdenticalState(t *testing.T) {
	l := bank(t)
	_ = l.Post(entry(t, "dep", line(t, "cash", Debit, 300, ars), line(t, "wallet", Credit, 300, ars)))
	_ = l.Reverse("dep", "rev", t0, "")
	history := append(openingEvents(t), l.Pending()...)
	r, err := Replay(history)
	if err != nil {
		t.Fatal(err)
	}
	l.MarkCommitted()
	if snapshot(t, r) != snapshot(t, l) || r.Version() != l.Version() {
		t.Fatalf("replay differs\n%s\n%s", snapshot(t, r), snapshot(t, l))
	}
}

func TestReplayDetectsCorruptHistory(t *testing.T) {
	open := openingEvents(t)
	dep := entry(t, "dep", line(t, "cash", Debit, 300, ars), line(t, "wallet", Credit, 300, ars))
	badRev, err := NewReversalEntry("rev", t0, "", "dep", line(t, "cash", Credit, 1, ars), line(t, "wallet", Debit, 1, ars))
	if err != nil {
		t.Fatal(err)
	}
	shortRev, err := NewReversalEntry("rev", t0, "", "dep", line(t, "cash", Credit, 300, ars), line(t, "wallet", Debit, 150, ars), line(t, "wallet", Debit, 150, ars))
	if err != nil {
		t.Fatal(err)
	}
	orphanRev, err := NewReversalEntry("rev", t0, "", "ghost", line(t, "cash", Credit, 1, ars), line(t, "wallet", Debit, 1, ars))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]Event{
		"nil event":         {nil},
		"duplicate account": slices.Concat(open, open[:1]),
		"unknown account":   {EntryPosted{Entry: dep}},
		"non-mirror":        slices.Concat(open, []Event{EntryPosted{Entry: dep}, EntryPosted{Entry: badRev}}),
		"length mismatch":   slices.Concat(open, []Event{EntryPosted{Entry: dep}, EntryPosted{Entry: shortRev}}),
		"orphan reversal":   slices.Concat(open, []Event{EntryPosted{Entry: orphanRev}}),
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Replay(h)
			wantErr(t, err, ErrCorruptHistory)
		})
	}
	// Replay does not re-evaluate overdraft policy (accepted facts stay accepted).
	overdraw := entry(t, "od", line(t, "wallet", Debit, 5, ars), line(t, "cash", Credit, 5, ars))
	if _, err := Replay(slices.Concat(open, []Event{EntryPosted{Entry: overdraw}})); err != nil {
		t.Fatalf("policy must not be re-checked on replay: %v", err)
	}
}

func TestBalanceOverflowGuards(t *testing.T) {
	l := NewLedger()
	_ = l.OpenAccount(account(t, "a", Asset, ars, Unrestricted()), t0)
	_ = l.OpenAccount(account(t, "l", Liability, ars, Unrestricted()), t0)
	big := entry(t, "b1", line(t, "a", Debit, math.MaxInt64, ars), line(t, "l", Credit, math.MaxInt64, ars))
	if err := l.Post(big); err != nil {
		t.Fatal(err)
	}
	// l is now -MaxInt64 signed; another credit of 1 reaches MinInt64 whose
	// natural (negated) balance does not fit in int64.
	wantErr(t, l.Post(entry(t, "b2", line(t, "a", Credit, 1, ars), line(t, "l", Credit, 1, ars), line(t, "x", Debit, 2, ars))), ErrUnknownAccount)
	wantErr(t, l.Post(entry(t, "b3", line(t, "a", Debit, 1, ars), line(t, "l", Credit, 1, ars))), ErrOverflow)
	_, err := l.Balance("ghost")
	wantErr(t, err, ErrUnknownAccount)
}

func TestNormalBalanceOverflowAtMinInt64(t *testing.T) {
	l := NewLedger()
	_ = l.OpenAccount(account(t, "a", Asset, ars, Unrestricted()), t0)
	_ = l.OpenAccount(account(t, "l", Liability, ars, Unrestricted()), t0)
	_ = l.OpenAccount(account(t, "l2", Liability, ars, Unrestricted()), t0)
	if err := l.Post(entry(t, "b1", line(t, "a", Debit, math.MaxInt64, ars), line(t, "l", Credit, math.MaxInt64, ars))); err != nil {
		t.Fatal(err)
	}
	// Moving 1 from l2 to l: l becomes MinInt64 signed; its negation overflows.
	wantErr(t, l.Post(entry(t, "b2", line(t, "l2", Debit, 1, ars), line(t, "l", Credit, 1, ars))), ErrOverflow)
	// Through replay (no policy check) the state can be reached and Balance reports overflow.
	h := []Event{
		AccountOpened{Account: account(t, "a", Asset, ars, Unrestricted()), OpenedAt: t0},
		AccountOpened{Account: account(t, "l", Liability, ars, Unrestricted()), OpenedAt: t0},
		AccountOpened{Account: account(t, "l2", Liability, ars, Unrestricted()), OpenedAt: t0},
		EntryPosted{Entry: entry(t, "b1", line(t, "a", Debit, math.MaxInt64, ars), line(t, "l", Credit, math.MaxInt64, ars))},
		EntryPosted{Entry: entry(t, "b2", line(t, "l2", Debit, 1, ars), line(t, "l", Credit, 1, ars))},
	}
	r, err := Replay(h)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Balance("l")
	wantErr(t, err, ErrOverflow)
	tb, err := r.TrialBalance()
	if err != nil || tb[ars] != 0 {
		t.Fatalf("trial balance with extreme partial sums: %v %v", tb, err)
	}
	// Replay with a delta that overflows the stored balance.
	h = append(h, EntryPosted{Entry: entry(t, "b3", line(t, "l2", Debit, 1, ars), line(t, "l", Credit, 1, ars))})
	_, err = Replay(h)
	wantErr(t, err, ErrCorruptHistory)
}

func TestInt128(t *testing.T) {
	var x int128
	x.add(math.MaxInt64)
	x.add(math.MaxInt64)
	if _, ok := x.int64(); ok {
		t.Fatal("2*MaxInt64 must not fit")
	}
	x.add(math.MinInt64)
	x.add(math.MinInt64)
	x.add(-5)
	if v, ok := x.int64(); !ok || v != -7 {
		t.Fatalf("got %d %v", v, ok)
	}
}
