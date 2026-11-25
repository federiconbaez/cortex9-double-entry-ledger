package domain

import (
	"math"
	"math/bits"
	"slices"
	"time"
)

// Ledger is the event-sourced aggregate root: one book of accounts and
// entries. State is never set directly; it is the fold of its events.
// Commands validate, raise an event, apply it and keep it pending until
// the Unit of Work commits it.
type Ledger struct {
	accounts map[AccountID]Account
	order    []AccountID
	balances map[AccountID]int64 // debit-positive
	entries  map[EntryID]JournalEntry
	reversed map[EntryID]EntryID // original -> reversal
	nEntries int
	version  uint64
	pending  []Event
	spec     Specification[BalanceProposal]
}

// NewLedger returns an empty ledger guarded by SufficientBalance.
func NewLedger() *Ledger {
	return &Ledger{
		accounts: map[AccountID]Account{},
		balances: map[AccountID]int64{},
		entries:  map[EntryID]JournalEntry{},
		reversed: map[EntryID]EntryID{},
		spec:     SufficientBalance(),
	}
}

// Replay rebuilds a ledger from its committed history. Structural
// invariants are re-checked; a violation is ErrCorruptHistory. Overdraft
// policy is not re-evaluated: it was enforced when the fact was accepted.
func Replay(history []Event) (*Ledger, error) {
	l := NewLedger()
	for i, ev := range history {
		if err := l.apply(ev, false); err != nil {
			return nil, errf(KindCorruptHistory, "event %d (%s): %v", i+1, eventType(ev), err)
		}
		l.version++
	}
	return l, nil
}

func eventType(ev Event) string {
	if ev == nil {
		return "<nil>"
	}
	return ev.EventType()
}

// Version is the number of committed events folded into this ledger.
func (l *Ledger) Version() uint64 { return l.version }

// Pending returns the uncommitted events, oldest first.
func (l *Ledger) Pending() []Event { return slices.Clone(l.pending) }

// MarkCommitted moves pending events into the committed version.
func (l *Ledger) MarkCommitted() {
	l.version += uint64(len(l.pending))
	l.pending = nil
}

// OpenAccount raises AccountOpened.
func (l *Ledger) OpenAccount(a Account, at time.Time) error {
	return l.raise(AccountOpened{Account: a, OpenedAt: at.UTC()})
}

// Post raises EntryPosted for an ordinary (non-reversal) entry.
func (l *Ledger) Post(e JournalEntry) error {
	if e.reversalOf != "" {
		return errf(KindInvalidID, "use Reverse to post reversal %s", e.id)
	}
	return l.raise(EntryPosted{Entry: e})
}

// Reverse posts the mirror of entry `of` under a new id (invariant I5).
func (l *Ledger) Reverse(of, id EntryID, at time.Time, memo string) error {
	orig, ok := l.entries[of]
	if !ok {
		return errf(KindUnknownEntry, "%s", of)
	}
	rev, err := orig.reversed(id, at, memo)
	if err != nil {
		return err
	}
	return l.raise(EntryPosted{Entry: rev})
}

func (l *Ledger) raise(ev Event) error {
	if err := l.apply(ev, true); err != nil {
		return err
	}
	l.pending = append(l.pending, ev)
	return nil
}

// apply validates ev completely and only then mutates state (invariant I10).
func (l *Ledger) apply(ev Event, enforcePolicy bool) error {
	switch e := ev.(type) {
	case AccountOpened:
		return l.applyOpened(e)
	case EntryPosted:
		return l.applyPosted(e.Entry, enforcePolicy)
	}
	return errf(KindCorruptHistory, "unknown event %T", ev)
}

func (l *Ledger) applyOpened(e AccountOpened) error {
	a := e.Account
	if _, err := NewAccount(a.id, a.name, a.typ, a.cur, a.policy); err != nil {
		return err
	}
	if _, dup := l.accounts[a.id]; dup {
		return errf(KindDuplicateAccount, "%s", a.id)
	}
	l.accounts[a.id] = a
	l.order = append(l.order, a.id)
	l.balances[a.id] = 0
	return nil
}

func (l *Ledger) applyPosted(e JournalEntry, enforcePolicy bool) error {
	if _, err := newEntry(e.id, e.postedAt, e.memo, e.reversalOf, e.lines); err != nil {
		return err // e.g. a zero-value JournalEntry
	}
	if _, dup := l.entries[e.id]; dup {
		return errf(KindDuplicateEntry, "%s", e.id)
	}
	if e.reversalOf != "" {
		if err := l.checkReversal(e); err != nil {
			return err
		}
	}
	deltas := map[AccountID]int64{}
	touched := []AccountID{}
	for _, ln := range e.lines {
		a, ok := l.accounts[ln.account]
		if !ok {
			return errf(KindUnknownAccount, "%s", ln.account)
		}
		if ln.amount.cur != a.cur {
			return errf(KindCurrencyMismatch, "account %s is %s, line is %s", a.id, a.cur, ln.amount.cur)
		}
		if _, seen := deltas[ln.account]; !seen {
			touched = append(touched, ln.account)
		}
		d, ok := addInt64(deltas[ln.account], ln.signed())
		if !ok {
			return errf(KindOverflow, "delta on %s", ln.account)
		}
		deltas[ln.account] = d
	}
	next := make(map[AccountID]int64, len(touched))
	for _, id := range touched {
		a := l.accounts[id]
		cur := l.balances[id]
		nb, ok := addInt64(cur, deltas[id])
		if !ok {
			return errf(KindOverflow, "balance of %s", id)
		}
		if enforcePolicy {
			curN, ok1 := a.normal(cur)
			nextN, ok2 := a.normal(nb)
			if !ok1 || !ok2 {
				return errf(KindOverflow, "normal balance of %s", id)
			}
			if !l.spec.IsSatisfiedBy(BalanceProposal{Account: a, Current: curN, Proposed: nextN}) {
				return errf(KindInsufficientFunds, "%s: %d -> %d under %s", id, curN, nextN, a.policy)
			}
		}
		next[id] = nb
	}
	for id, b := range next {
		l.balances[id] = b
	}
	l.entries[e.id] = e
	l.nEntries++
	if e.reversalOf != "" {
		l.reversed[e.reversalOf] = e.id
	}
	return nil
}

func (l *Ledger) checkReversal(rev JournalEntry) error {
	orig, ok := l.entries[rev.reversalOf]
	if !ok {
		return errf(KindUnknownEntry, "reversal %s of %s", rev.id, rev.reversalOf)
	}
	if orig.reversalOf != "" {
		return errf(KindAlreadyReversed, "%s is itself a reversal", orig.id)
	}
	if by, done := l.reversed[orig.id]; done {
		return errf(KindAlreadyReversed, "%s already reversed by %s", orig.id, by)
	}
	if len(orig.lines) != len(rev.lines) {
		return errf(KindCorruptHistory, "reversal %s does not mirror %s", rev.id, orig.id)
	}
	for i, ol := range orig.lines {
		rl := rev.lines[i]
		if rl.account != ol.account || rl.amount != ol.amount || rl.side == ol.side {
			return errf(KindCorruptHistory, "reversal %s does not mirror %s", rev.id, orig.id)
		}
	}
	return nil
}

// Account returns an account by id.
func (l *Ledger) Account(id AccountID) (Account, bool) {
	a, ok := l.accounts[id]
	return a, ok
}

// Accounts returns accounts in opening order.
func (l *Ledger) Accounts() []Account {
	out := make([]Account, 0, len(l.order))
	for _, id := range l.order {
		out = append(out, l.accounts[id])
	}
	return out
}

// Entry returns an entry by id.
func (l *Ledger) Entry(id EntryID) (JournalEntry, bool) {
	e, ok := l.entries[id]
	return e, ok
}

// EntryCount returns the number of posted entries (reversals included).
func (l *Ledger) EntryCount() int { return l.nEntries }

// SignedBalance returns the debit-positive balance of an account.
func (l *Ledger) SignedBalance(id AccountID) (Money, error) {
	a, ok := l.accounts[id]
	if !ok {
		return Money{}, errf(KindUnknownAccount, "%s", id)
	}
	return Money{minor: l.balances[id], cur: a.cur}, nil
}

// Balance returns the balance in the account's natural sign (positive
// means "has money" for an asset, "owes money" for a liability).
func (l *Ledger) Balance(id AccountID) (Money, error) {
	s, err := l.SignedBalance(id)
	if err != nil {
		return Money{}, err
	}
	n, ok := l.accounts[id].normal(s.minor)
	if !ok {
		return Money{}, errf(KindOverflow, "normal balance of %s", id)
	}
	return Money{minor: n, cur: s.cur}, nil
}

// TrialBalance returns, per currency, the sum of debit-positive balances.
// Invariant I6: every value is 0. The sum is accumulated in 128 bits so a
// transient partial sum beyond int64 is not a false overflow; only a
// final total outside int64 (impossible for balanced history) errors.
func (l *Ledger) TrialBalance() (map[Currency]int64, error) {
	acc := map[Currency]*int128{}
	for _, id := range l.order {
		c := l.accounts[id].cur
		if acc[c] == nil {
			acc[c] = &int128{}
		}
		acc[c].add(l.balances[id])
	}
	out := make(map[Currency]int64, len(acc))
	for c, v := range acc {
		n, ok := v.int64()
		if !ok {
			return nil, errf(KindOverflow, "trial balance %s", c)
		}
		out[c] = n
	}
	return out, nil
}

type int128 struct {
	hi int64
	lo uint64
}

func (x *int128) add(v int64) {
	var carry uint64
	x.lo, carry = bits.Add64(x.lo, uint64(v), 0) //nolint:gosec // two's complement reinterpretation is intended
	x.hi += int64(carry)                         //nolint:gosec // carry is 0 or 1
	if v < 0 {
		x.hi--
	}
}

func (x *int128) int64() (int64, bool) {
	switch {
	case x.hi == 0 && x.lo <= math.MaxInt64:
		return int64(x.lo), true
	case x.hi == -1 && x.lo >= 1<<63:
		return int64(x.lo), true //nolint:gosec // in range by the case guard
	}
	return 0, false
}
